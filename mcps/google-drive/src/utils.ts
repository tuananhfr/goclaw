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

export function boolEnvOrHeader(headers: Record<string, any>, env: NodeJS.ProcessEnv, header: string, envKey: string, fallback: boolean): boolean {
  const raw = envOrHeader(headers, env, header, envKey);
  if (!raw) return fallback;
  return ["1", "true", "yes", "on"].includes(raw.toLowerCase());
}

export function jsonEnvOrHeader<T>(headers: Record<string, any>, env: NodeJS.ProcessEnv, header: string, envKey: string, fallback: T): T {
  const raw = envOrHeader(headers, env, header, envKey);
  if (!raw) return fallback;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
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
    case "application/vnd.google-apps.document":
    case "application/vnd.google-apps.presentation":
      return ".pdf";
    case "application/vnd.google-apps.spreadsheet":
      return ".xlsx";
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
    case "application/pdf":
      return ".pdf";
    case "text/plain":
      return ".txt";
    default:
      return ".bin";
  }
}

export function parseDriveID(input: string): string {
  const raw = String(input ?? "").trim();
  try {
    const u = new URL(raw);
    const fileMatch = u.pathname.match(/\/file\/d\/([^/]+)/);
    if (fileMatch?.[1]) return fileMatch[1];
    const folderMatch = u.pathname.match(/\/folders\/([^/]+)/);
    if (folderMatch?.[1]) return folderMatch[1];
    const id = u.searchParams.get("id");
    if (id) return id;
  } catch {
    // Not a URL; treat as an ID.
  }
  return raw;
}
