import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { tokenize } from "./text.js";

/**
 * Nhớ "cách hỏi này -> capability kia" để lần sau khỏi phải tìm lại.
 *
 * =============================================================================
 * VÌ SAO CẦN
 * =============================================================================
 * Mỗi lượt `erp_find` là một lượt suy luận của mô hình (5-10 giây). Người dùng
 * hỏi lặp lại rất nhiều ("báo cáo t8", rồi "báo cáo t9"), vậy mà lần nào cũng
 * tìm lại từ đầu và có thể trượt. Kho này ghi lại lựa chọn ĐÃ RA DỮ LIỆU để lần
 * sau mô hình đi thẳng.
 *
 * =============================================================================
 * KHÔNG NỚI QUYỀN
 * =============================================================================
 * Kho chỉ gợi ý key, không cấp key: mọi gợi ý đều bị lọc lại theo catalog của
 * kết nối hiện tại, nên người vừa mất quyền không thấy capability cũ nữa.
 *
 * Khoá người dùng là băm của token vì sidecar không biết uid. Token bị xoay
 * (gán lại agent) thì người đó bắt đầu lại với kho trống — chấp nhận được, hiếm.
 *
 * Chỉ có kho RIÊNG, cố ý không có kho chung: câu hỏi chứa tên người, tên dự án,
 * và gộp chung là để lọt chúng sang kho của người khác. Người mới chưa có thói
 * quen thì đã có mục lục trong mô tả erp_get.
 */

/** Điểm giảm một nửa sau chừng này ngày không dùng. */
const HALF_LIFE_DAYS = 30;
const DAY_MS = 86_400_000;
const MAX_INTENTS_PER_USER = 200;
const MAX_USERS = 5_000;
const SAVE_DELAY_MS = 5_000;
/** Chồng từ tối thiểu (Jaccard) để coi hai câu là cùng ý. */
const MIN_SIMILARITY = 0.5;

// Mốc thời gian đổi theo từng câu nhưng API cần gọi thì không: "báo cáo t8" và
// "báo cáo tháng 9 năm nay" phải về cùng một khoá.
const TIME_WORDS = new Set([
  "thang", "nam", "ngay", "tuan", "quy", "hom", "qua", "truoc", "gan", "day",
  "nhat", "moi", "cu", "sau", "gio",
]);

interface Hit {
  n: number;
  last: number;
}

interface IntentEntry {
  /** Câu mô hình viết lần gần nhất, chỉ để hiện lại cho CHÍNH người đó. */
  sample?: string;
  caps: Record<string, Hit>;
}

type IntentMap = Record<string, IntentEntry>;

interface Store {
  version: 2;
  users: Record<string, IntentMap>;
}

export interface RouteSuggestion {
  capability: string;
  score: number;
}

export interface Favorite {
  intent: string;
  capability: string;
}

/** Khoá ý định: từ khoá đã bỏ dấu, bỏ hư từ và mốc thời gian, sắp xếp, bỏ trùng. */
export function intentKey(text: unknown): string {
  const tokens = tokenize(text).filter((token) => !/\d/.test(token) && !TIME_WORDS.has(token));
  return [...new Set(tokens)].sort().slice(0, 8).join(" ");
}

export class RouteMemory {
  private store: Store = { version: 2, users: {} };
  private saveTimer: NodeJS.Timeout | undefined;

  /** `file` trống = chỉ giữ trong RAM (test, hoặc chưa gắn volume). */
  constructor(private readonly file = "", private readonly now: () => number = Date.now) {
    if (file !== "") this.load();
  }

  /** Ghi nhận một lần gọi ĐÃ RA DỮ LIỆU. */
  record(userKey: string, intent: string, capability: string): void {
    const key = intentKey(intent);
    if (key === "" || capability === "") return;

    const users = this.store.users;
    if (!users[userKey] && Object.keys(users).length >= MAX_USERS) return;
    const mine = (users[userKey] ??= {});

    bump(mine, key, capability, this.now(), intent.trim().slice(0, 80));
    prune(mine, MAX_INTENTS_PER_USER, this.now());
    this.scheduleSave();
  }

  /** Capability đã từng trả lời những câu giống `query` của người này, điểm cao trước. */
  suggest(userKey: string, query: string, allowed: ReadonlySet<string>): RouteSuggestion[] {
    const tokens = intentKey(query).split(" ").filter(Boolean);
    if (tokens.length === 0) return [];

    const scores = new Map<string, number>();
    for (const [key, entry] of Object.entries(this.store.users[userKey] ?? {})) {
      const similarity = jaccard(tokens, key.split(" "));
      if (similarity < MIN_SIMILARITY) continue;
      for (const [capability, hit] of Object.entries(entry.caps)) {
        if (!allowed.has(capability)) continue;
        const score = similarity * decayed(hit, this.now());
        scores.set(capability, (scores.get(capability) ?? 0) + score);
      }
    }

    return [...scores]
      .map(([capability, score]) => ({ capability, score }))
      .sort((a, b) => b.score - a.score);
  }

  /**
   * Những API người này hay dùng, để in sẵn vào mô tả tool.
   *
   * Gộp điểm theo capability: "dự án đang chạy" và "dự án của tôi" là hai khoá
   * nhưng cùng một thói quen. Mỗi capability hiện một lần, kèm câu hỏi dùng nhiều
   * nhất của nó.
   */
  favorites(userKey: string, allowed: ReadonlySet<string>, limit = 10): Favorite[] {
    const byCapability = new Map<string, { total: number; intent: string; best: number; last: number }>();
    for (const [key, entry] of Object.entries(this.store.users[userKey] ?? {})) {
      for (const [capability, hit] of Object.entries(entry.caps)) {
        if (!allowed.has(capability)) continue;
        const score = decayed(hit, this.now());
        const row = byCapability.get(capability) ?? { total: 0, intent: "", best: -1, last: -1 };
        row.total += score;
        if (score > row.best || (score === row.best && hit.last > row.last)) {
          row.best = score;
          row.last = hit.last;
          row.intent = entry.sample || key;
        }
        byCapability.set(capability, row);
      }
    }
    return [...byCapability]
      .sort((a, b) => b[1].total - a[1].total)
      .slice(0, limit)
      .map(([capability, row]) => ({ intent: row.intent, capability }));
  }

  /** Ghi ngay, bỏ qua hẹn giờ. Gọi lúc tắt tiến trình. */
  flush(): void {
    if (this.saveTimer) clearTimeout(this.saveTimer);
    this.saveTimer = undefined;
    if (this.file === "") return;
    try {
      mkdirSync(dirname(this.file), { recursive: true });
      // Ghi ra tệp tạm rồi đổi tên: tắt ngang lúc đang ghi thì vẫn còn bản cũ
      // nguyên vẹn, không phải một tệp JSON cụt làm mất sạch kho.
      const tmp = `${this.file}.tmp`;
      writeFileSync(tmp, JSON.stringify(this.store));
      renameSync(tmp, this.file);
    } catch (err) {
      console.warn("[route-memory] Không ghi được:", err instanceof Error ? err.message : err);
    }
  }

  private scheduleSave(): void {
    if (this.file === "" || this.saveTimer) return;
    this.saveTimer = setTimeout(() => this.flush(), SAVE_DELAY_MS);
    this.saveTimer.unref();
  }

  private load(): void {
    try {
      const parsed = JSON.parse(readFileSync(this.file, "utf8")) as Partial<Store>;
      if (parsed.version === 2) {
        this.store = { version: 2, users: parsed.users ?? {} };
      }
    } catch (err) {
      // Chưa có tệp là bình thường (lần chạy đầu). Tệp hỏng thì bắt đầu lại:
      // kho này chỉ để tăng tốc, mất đi không sai dữ liệu nào.
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
        console.warn("[route-memory] Bỏ qua tệp hỏng:", err instanceof Error ? err.message : err);
      }
    }
  }
}

function bump(map: IntentMap, key: string, capability: string, now: number, sample?: string): void {
  const entry = (map[key] ??= { caps: {} });
  if (sample) entry.sample = sample;
  const hit = (entry.caps[capability] ??= { n: 0, last: now });
  hit.n += 1;
  hit.last = now;
}

/** Quá trần thì bỏ những ý định có điểm thấp nhất. */
function prune(map: IntentMap, max: number, now: number): void {
  const keys = Object.keys(map);
  if (keys.length <= max) return;
  const strength = (key: string) =>
    Math.max(...Object.values(map[key].caps).map((hit) => decayed(hit, now)));
  keys
    .sort((a, b) => strength(a) - strength(b))
    .slice(0, keys.length - max)
    .forEach((key) => delete map[key]);
}

function decayed(hit: Hit, now: number): number {
  const ageDays = Math.max(0, now - hit.last) / DAY_MS;
  return hit.n * 0.5 ** (ageDays / HALF_LIFE_DAYS);
}

function jaccard(a: string[], b: string[]): number {
  const left = new Set(a);
  const right = new Set(b);
  let shared = 0;
  for (const token of left) if (right.has(token)) shared++;
  return shared / (left.size + right.size - shared);
}
