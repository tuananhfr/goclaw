/**
 * Hình dạng catalog do Drupal trả về (`GET /api/v1/assistant/tools`).
 *
 * Sidecar KHÔNG biết ERPcons có những API nào — nó chỉ biết cách đọc mô tả này
 * rồi dựng tool. Thêm một API cho trợ lý là sửa `tools/catalog.yml` bên Drupal,
 * không phải build lại image này.
 */

export interface CatalogParam {
  type: string;
  description: string;
  required: boolean;
  /** `path` = ghép vào đường dẫn; `query` = đưa vào chuỗi truy vấn. */
  in: "path" | "query";
  enum?: (string | number)[];
  format?: string;
}

export interface TrimRule {
  max_items?: number;
  /** Danh sách TRẮNG các trường được giữ. Vắng = giữ nguyên phần tử. */
  keep?: string[];
}

export interface CatalogTool {
  description: string;
  path: string;
  params?: Record<string, CatalogParam>;
  /** Khoá cấp một -> luật cắt gọn. */
  trim?: Record<string, TrimRule>;
  /** Khoá cấp một bị bỏ hẳn khỏi phản hồi. */
  drop?: string[];
}

export type Catalog = Record<string, CatalogTool>;

export interface CatalogResponse {
  ok?: boolean;
  tools?: Catalog;
}
