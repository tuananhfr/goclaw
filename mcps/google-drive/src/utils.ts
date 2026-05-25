export function envOrHeader(headers: Record<string, any>, env: NodeJS.ProcessEnv, header: string, envKey: string): string {
  const raw = headers[header.toLowerCase()] ?? headers[header] ?? env[envKey] ?? "";
  return String(raw ?? "").trim();
}

export function intEnvOrHeader(headers: Record<string, any>, env: NodeJS.ProcessEnv, header: string, envKey: string, fallback: number): number {
  const raw = envOrHeader(headers, env, header, envKey);
  if (!raw) return fallback;
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export function normalizeName(value: string): string {
  return value
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim()
    .replace(/\s+/g, " ");
}

export function safeSegment(value: string): string {
  return value.replace(/[^a-zA-Z0-9._-]+/g, "_").replace(/^_+|_+$/g, "") || "item";
}

export function parseNumber(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function fileExt(name: string, mimeType: string): string {
  const dot = name.lastIndexOf(".");
  if (dot > -1 && dot < name.length - 1) {
    return name.slice(dot).toLowerCase();
  }
  switch (mimeType) {
    case "image/jpeg":
      return ".jpg";
    case "image/png":
      return ".png";
    case "image/webp":
      return ".webp";
    case "image/gif":
      return ".gif";
    case "image/avif":
      return ".avif";
    default:
      return ".img";
  }
}
