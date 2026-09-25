package tekshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestClampReplyDataTable(t *testing.T) {
	cols := make([]string, 35)
	for i := range cols {
		cols[i] = strings.Repeat("h", 150)
	}
	row := make([]string, 35)
	row[0] = strings.Repeat("x", 600)
	rows := make([][]string, 2100)
	for i := range rows {
		rows[i] = row
	}
	got, ok := clampReplyDataTable(replyDataTable{Name: strings.Repeat("n", 300), Columns: cols, Rows: rows, Uncertain: [][2]int{{0, 0}, {2050, 1}, {1, 40}}})
	if !ok || len(got.Columns) != 30 || len(got.Rows) != 2000 || len(got.Rows[0]) != 30 {
		t.Fatalf("limits not applied: cols=%d rows=%d", len(got.Columns), len(got.Rows))
	}
	// clampRunes cuts with no ellipsis — the cross-layer contract (cell 500, column
	// 120, name 255) is exact, and Go is the only layer that enforces it.
	if len([]rune(got.Rows[0][0])) != 500 {
		t.Fatalf("cell not cut to exactly 500 runes, got %d", len([]rune(got.Rows[0][0])))
	}
	if len([]rune(got.Columns[0])) != 120 {
		t.Fatalf("column not cut to exactly 120 runes, got %d", len([]rune(got.Columns[0])))
	}
	if len([]rune(got.Name)) != 255 {
		t.Fatalf("name not cut to exactly 255 runes, got %d", len([]rune(got.Name)))
	}
	if len(got.Uncertain) != 1 {
		t.Fatalf("out-of-range uncertain cells must be dropped, got %v", got.Uncertain)
	}
	if _, ok := clampReplyDataTable(replyDataTable{Columns: []string{"a"}}); ok {
		t.Fatal("a table without rows is not a table")
	}
}

func TestReplyDataTableToolCapturesTable(t *testing.T) {
	tool := NewReplyDataTableTool()
	res := tool.Execute(context.Background(), map[string]any{
		"has_table": true, "name": "Menu ảnh",
		"columns":   []any{"Tên món", "Giá"},
		"rows":      []any{[]any{"Bánh rán", "15000"}, []any{"Bánh chưng"}},
		"uncertain": []any{map[string]any{"row": float64(1), "column": float64(1)}},
	})
	if res.IsError {
		t.Fatalf("unexpected error %s", res.ForLLM)
	}
	got := tool.Table()
	if got == nil || got.Rows[1][1] != "" || got.Uncertain[0] != [2]int{1, 1} || got.Source != "vision" {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestReplyDataTableToolNoTable(t *testing.T) {
	tool := NewReplyDataTableTool()
	tool.Execute(context.Background(), map[string]any{"has_table": false, "name": "", "columns": []any{}, "rows": []any{}, "uncertain": []any{}})
	if tool.Table() != nil || !tool.Submitted() {
		t.Fatal("has_table=false must count as submitted with no table")
	}
}

func TestReadTablesFromImagesSkipsFailuresAndEmpty(t *testing.T) {
	imgs := []extractedImage{{Ref: "Trang 1"}, {Ref: "Trang 2"}, {Ref: "Trang 3"}}
	run := func(_ context.Context, img extractedImage) (*replyDataTable, error) {
		switch img.Ref {
		case "Trang 1":
			return &replyDataTable{Name: "Menu", Columns: []string{"A"}, Rows: [][]string{{"1"}}}, nil
		case "Trang 2":
			return nil, errors.New("boom")
		}
		return nil, nil
	}
	tables, unread := readTablesFromImages(context.Background(), imgs, "menu.xlsx", run)
	if len(tables) != 1 || tables[0].Name != "Trang 1 · Menu" || tables[0].Source != "vision" {
		t.Fatalf("tables %+v", tables)
	}
	if len(unread) != 1 || unread[0] != "Trang 2" {
		t.Fatalf("unread %v", unread)
	}
}

func TestReadTablesFromImagesSingleImageUsesNameOnly(t *testing.T) {
	imgs := []extractedImage{{Ref: "menu.jpg"}}
	run := func(_ context.Context, img extractedImage) (*replyDataTable, error) {
		return &replyDataTable{Name: "Bảng giá", Columns: []string{"A"}, Rows: [][]string{{"1"}}}, nil
	}
	tables, _ := readTablesFromImages(context.Background(), imgs, "menu.jpg", run)
	if len(tables) != 1 || tables[0].Name != "Bảng giá" {
		t.Fatalf("single-image ref must not repeat the filename, got %+v", tables)
	}
}

func TestReadTablesFromImagesSingleImageEmptyNameFallsBack(t *testing.T) {
	imgs := []extractedImage{{Ref: "menu.jpg"}}
	run := func(_ context.Context, img extractedImage) (*replyDataTable, error) {
		return &replyDataTable{Name: "", Columns: []string{"A"}, Rows: [][]string{{"1"}}}, nil
	}
	tables, _ := readTablesFromImages(context.Background(), imgs, "menu.jpg", run)
	if len(tables) != 1 || tables[0].Name != "Bảng" {
		t.Fatalf("empty vision name on a single image must fall back to a plain label, got %+v", tables)
	}
}

func TestReadTablesFromImagesMultiPageEmptyNameHasNoDanglingSeparator(t *testing.T) {
	imgs := []extractedImage{{Ref: "Trang 1"}}
	run := func(_ context.Context, img extractedImage) (*replyDataTable, error) {
		return &replyDataTable{Name: "", Columns: []string{"A"}, Rows: [][]string{{"1"}}}, nil
	}
	tables, _ := readTablesFromImages(context.Background(), imgs, "menu.jpg", run)
	if len(tables) != 1 || tables[0].Name != "Trang 1" {
		t.Fatalf("empty name on a multi-page ref must not leave a dangling separator, got %+v", tables)
	}
}

func TestReplyDataExtractIsSupported(t *testing.T) {
	if !isSupportedTekshotJobType(TekshotJobTypeReplyDataExtract) {
		t.Fatal("reply_data_extract must be accepted at job creation")
	}
}

func TestRunReplyDataExtractNoTables(t *testing.T) {
	s := &JobService{}
	res, progress, err := s.replyDataResult(context.Background(), &tableExtraction{OK: true}, "", func(context.Context, extractedImage) (*replyDataTable, error) { return nil, nil })
	if err != nil || progress != "Đã tách 0 bảng" {
		t.Fatalf("err=%v progress=%q", err, progress)
	}
	if got := res.(map[string]any)["tables"].([]replyDataTable); len(got) != 0 || got == nil {
		t.Fatal("tables must be an empty array, not null, so Drupal sees [] ")
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"unread_images":[]`) {
		t.Fatalf("unread_images must encode as [] not null: %s", encoded)
	}
}

func TestRunReplyDataExtractCancelledContext(t *testing.T) {
	s := &JobService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ext := &tableExtraction{OK: true, Images: []extractedImage{{Ref: "Trang 1"}}}
	_, _, err := s.replyDataResult(ctx, ext, "", func(context.Context, extractedImage) (*replyDataTable, error) {
		t.Fatal("must not run vision on an already-cancelled context")
		return nil, nil
	})
	if err == nil {
		t.Fatal("a cancelled/timed-out job must return an error, not complete over the cancellation")
	}
}

func TestClampReplyDataTableUncertainIsEmptyArrayNotNull(t *testing.T) {
	got, ok := clampReplyDataTable(replyDataTable{Columns: []string{"A"}, Rows: [][]string{{"1"}}})
	if !ok {
		t.Fatal("expected a table")
	}
	if got.Uncertain == nil {
		t.Fatal("Uncertain must be an empty slice, not nil, so Drupal sees []")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"uncertain":[]`) {
		t.Fatalf("uncertain must encode as [] not null: %s", encoded)
	}
}
