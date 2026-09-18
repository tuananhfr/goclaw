// Hư từ xuất hiện ở gần như mọi câu hỏi; giữ lại thì mọi capability đều "khớp".
// So SAU khi bỏ dấu, nên không đưa vào từ trùng với từ nghiệp vụ ("ban" = bán,
// "the" = thẻ, "do" = độ).
const STOPWORDS = new Set([
  "cua", "toi", "minh", "cho", "cac", "nhung", "la", "va", "voi", "trong",
  "nao", "co", "khong", "gi", "nhe", "xem", "lay", "hay", "giup", "duoc",
  "mot", "nay", "o", "tu", "den", "theo",
]);

/** Chữ thường, bỏ dấu (kể cả "đ"), tách theo ký tự không phải chữ/số. */
export function tokenize(value: unknown): string[] {
  return normalize(value)
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/đ/g, "d")
    .split(/[^\p{L}\p{N}]+/u)
    .filter((token) => token !== "" && !STOPWORDS.has(token));
}

export function normalize(value: unknown): string {
  return String(value ?? "").trim().toLocaleLowerCase("vi");
}
