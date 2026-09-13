import { createHash } from "node:crypto";
import type { Catalog } from "./types.js";

/**
 * Nhớ catalog của từng token trong thời gian ngắn.
 *
 * =============================================================================
 * VÌ SAO CẦN
 * =============================================================================
 * Mỗi lần GoClaw mở kết nối SSE là một lượt gọi `/assistant/tools` sang Drupal,
 * tức một lượt khởi động PHP. Đo trên dev: 77ms - 1.198ms, có lần 8.862ms, và
 * một lần quá 20 giây khiến sidecar phải từ chối kết nối bằng 503 rồi GoClaw
 * thử lại. Khoản đó nằm đúng trên đường đi của câu hỏi - người dùng ngồi chờ.
 *
 * HẠN PHẢI TRƯỢT, KHÔNG CỐ ĐỊNH: kết nối chỉ mở lại sau khi đã nhàn rỗi hết
 * `GOCLAW_MCP_USER_IDLE_TTL` (15 phút), mà bản cache thì nạp từ lúc mở kết nối
 * TRƯỚC - với người chat cả buổi, nó đã già hàng giờ. Hạn cố định ngắn hơn
 * khoảng đó là tỉ lệ trúng gần bằng không, đúng lúc cần nhất.
 *
 * =============================================================================
 * CHỈ CACHE KẾT QUẢ THÀNH CÔNG
 * =============================================================================
 * Lỗi phải ném ra để `index.ts` từ chối kết nối bằng 503. Nhớ một lần lỗi rồi
 * trả lại nó chính là tự dựng lại cái bẫy "0 tool im lặng": GoClaw sẽ cache một
 * kết nối khoẻ mà rỗng, và người dùng mất khả năng tra cứu cho tới khi có ai đó
 * khởi động lại thứ gì đó.
 *
 * Đánh đổi: quyền vừa đổi bên Drupal trễ tối đa `ttlMs` mới tới trợ lý. Riêng
 * token bị thu hồi thì không trễ - `index.ts` xoá entry ngay khi gặp 401.
 */

const DEFAULT_TTL_MS = 15 * 60_000;

/**
 * Trần tuyệt đối, tính từ lúc NẠP.
 *
 * Hạn trượt ở trên giữ bản cache sống chừng nào còn dùng, nên người chat cả
 * ngày sẽ dùng mãi một bản - quyền mới cấp không bao giờ tới nơi. Trần này buộc
 * hỏi lại Drupal ít nhất mỗi giờ. Thu hồi thì không phải đợi: 401 xoá ngay.
 */
const MAX_AGE_MS = 60 * 60_000;

/** Trần số entry. Một người một entry; quá trần thì dọn hết cho gọn. */
const MAX_ENTRIES = 2_000;

interface Entry {
  catalog: Catalog;
  /** Hạn trượt: mỗi lần trúng lại đẩy ra xa. */
  expiresAt: number;
  /** Mốc nạp, để áp trần tuyệt đối. */
  createdAt: number;
}

const entries = new Map<string, Entry>();

function ttlMs(): number {
  const raw = Number(process.env.ERP_CATALOG_CACHE_MS);
  return Number.isFinite(raw) && raw >= 0 ? raw : DEFAULT_TTL_MS;
}

/* Khoá là BĂM của token: kho này nằm trong RAM cùng tiến trình, không có lý do
   để giữ token thô ở thêm một chỗ nữa. */
function keyOf(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

/** Catalog còn hạn của token, hoặc `undefined` nếu chưa có / đã hết hạn. */
export function getCachedCatalog(token: string): Catalog | undefined {
  const ttl = ttlMs();
  if (token === "" || ttl === 0) return undefined;

  const key = keyOf(token);
  const hit = entries.get(key);
  if (!hit) return undefined;

  const now = Date.now();
  if (hit.expiresAt <= now || now - hit.createdAt >= MAX_AGE_MS) {
    entries.delete(key);
    return undefined;
  }

  hit.expiresAt = now + ttl;
  return hit.catalog;
}

export function setCachedCatalog(token: string, catalog: Catalog): void {
  const ttl = ttlMs();
  if (token === "" || ttl === 0) return;

  // Dọn entry hết hạn lúc ghi: không có bộ đếm giờ nào chạy nền, và số entry
  // bằng số người đang chat nên vòng lặp này luôn ngắn.
  const now = Date.now();
  for (const [key, entry] of entries) {
    if (entry.expiresAt <= now) entries.delete(key);
  }
  if (entries.size >= MAX_ENTRIES) entries.clear();

  entries.set(keyOf(token), { catalog, expiresAt: now + ttl, createdAt: now });
}

/** Quên catalog của một token. Gọi khi ERPcons trả 401 (token đã bị xoay). */
export function dropCachedCatalog(token: string): void {
  if (token === "") return;
  entries.delete(keyOf(token));
}
