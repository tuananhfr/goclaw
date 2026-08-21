package tekshot

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// referenceLibraryItem is one entry of the request's reference_library
// manifest. Drupal only lists images that already have a description
// (StudioReferenceImageRepository::manifestForStudio), and only when the user
// ticked "use reference library" — so presence of entries IS the opt-in.
type referenceLibraryItem struct {
	ID          int
	URL         string
	Description string
}

func referenceLibraryFromRequest(request map[string]any) []referenceLibraryItem {
	raw, ok := request["reference_library"].([]any)
	if !ok {
		return nil
	}
	items := make([]referenceLibraryItem, 0, len(raw))
	for _, entry := range raw {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id := int(numberFromMap(record, "id"))
		url := strings.TrimSpace(stringFromMap(record, "url"))
		description := strings.TrimSpace(stringFromMap(record, "description"))
		if id <= 0 || url == "" || description == "" {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			continue
		}
		items = append(items, referenceLibraryItem{ID: id, URL: url, Description: description})
	}
	return items
}

// referenceLibraryMediaFiles turns manifest entries into URL attachments. The
// agent loop's persistMedia downloads each URL into the run workspace's
// .uploads/ dir, keeping the "ref-lib-<id>" stem in the persisted filename —
// which is how the manifest block below and the <media:image path="..."> tags
// stay linkable. MimeType is only a hint for the media TAG kind (the tag must
// render as <media:image> so path enrichment can find it); the real MIME is
// re-detected from the download, and image sanitising re-encodes to JPEG.
func referenceLibraryMediaFiles(items []referenceLibraryItem) []bus.MediaFile {
	files := make([]bus.MediaFile, 0, len(items))
	for _, item := range items {
		mimeType := media.DetectMIMEType(item.URL)
		if !strings.HasPrefix(mimeType, "image/") {
			mimeType = "image/jpeg"
		}
		files = append(files, bus.MediaFile{
			Path:     item.URL,
			MimeType: mimeType,
			Filename: fmt.Sprintf("ref-lib-%d", item.ID),
		})
	}
	return files
}

// buildChosenReferenceBlock names the one library image the selection pass
// already picked. The path indirection is deliberate: the runner cannot know
// the persisted path up front (persistMedia appends a random suffix), but the
// pipeline's enrichImageIDs stamps the real path onto the <media:image> tag.
func buildChosenReferenceBlock(item referenceLibraryItem) string {
	var sb strings.Builder
	sb.WriteString("## Reference image from the store library (attached)\n")
	sb.WriteString(fmt.Sprintf("One image was pre-selected from the store's curated library for this brief: ref-lib-%d.\n", item.ID))
	sb.WriteString("Catalogue description: " + item.Description + "\n")
	sb.WriteString("When calling create_image, set reference_image_path to the path=\"...\" value of the <media:image> tag whose path contains \"ref-lib-\".\n")
	return sb.String()
}
