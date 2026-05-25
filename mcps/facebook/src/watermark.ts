import sharp from "sharp";

export interface WatermarkConfig {
  enabled?: boolean;
  items?: WatermarkConfig[];
  mode?: "logo" | "text";
  text?: string;
  logo_path?: string;
  logo_url?: string;
  x_pct?: number;
  y_pct?: number;
  scale_pct?: number;
  opacity?: number;
}

export interface WatermarkAssetLoader {
  (pathOrUrl: string): Promise<Buffer>;
}

function clamp(v: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, v));
}

function svgText(text: string, width: number, opacity: number): Buffer {
  const safe = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  const fontSize = Math.max(16, Math.round(width * 0.22));
  const height = Math.max(48, Math.round(fontSize * 1.8));
  return Buffer.from(`
    <svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" xmlns="http://www.w3.org/2000/svg">
      <rect x="0" y="0" width="${width}" height="${height}" rx="${Math.round(height * 0.18)}" fill="black" opacity="${opacity * 0.35}"/>
      <text x="50%" y="52%" dominant-baseline="middle" text-anchor="middle"
        font-family="Arial, Helvetica, sans-serif" font-size="${fontSize}" font-weight="700"
        fill="white" opacity="${opacity}">${safe}</text>
    </svg>
  `);
}

async function trimTransparentPadding(input: Buffer): Promise<Buffer> {
  const image = sharp(input, { failOn: "none" }).rotate().ensureAlpha();
  const { data, info } = await image.raw().toBuffer({ resolveWithObject: true });
  const channels = info.channels;
  if (channels < 4 || info.width <= 0 || info.height <= 0) {
    return input;
  }

  let minX = info.width;
  let minY = info.height;
  let maxX = -1;
  let maxY = -1;
  const alphaOffset = channels - 1;
  const alphaThreshold = 8;

  for (let y = 0; y < info.height; y++) {
    const row = y * info.width * channels;
    for (let x = 0; x < info.width; x++) {
      const alpha = data[row + x * channels + alphaOffset];
      if (alpha <= alphaThreshold) continue;
      if (x < minX) minX = x;
      if (y < minY) minY = y;
      if (x > maxX) maxX = x;
      if (y > maxY) maxY = y;
    }
  }

  if (maxX < 0) {
    return input;
  }

  const width = maxX - minX + 1;
  const height = maxY - minY + 1;
  if (minX === 0 && minY === 0 && width === info.width && height === info.height) {
    return input;
  }

  return sharp(input, { failOn: "none" })
    .rotate()
    .extract({ left: minX, top: minY, width, height })
    .png()
    .toBuffer();
}

export async function applyWatermark(
  input: Buffer,
  cfg: WatermarkConfig | undefined,
  loadAsset: WatermarkAssetLoader,
): Promise<{ data: Buffer; contentType: string }> {
  if (!cfg?.enabled) {
    return { data: input, contentType: "image/jpeg" };
  }

  if (cfg.items?.length) {
    let current = input;
    for (const item of cfg.items) {
      if (!item.enabled) continue;
      const out = await applySingleWatermark(current, item, loadAsset);
      current = out.data;
    }
    return { data: current, contentType: "image/jpeg" };
  }

  return applySingleWatermark(input, cfg, loadAsset);
}

async function applySingleWatermark(
  input: Buffer,
  cfg: WatermarkConfig,
  loadAsset: WatermarkAssetLoader,
): Promise<{ data: Buffer; contentType: string }> {
  const base = sharp(input, { failOn: "none" }).rotate();
  const meta = await base.metadata();
  const imageWidth = meta.width ?? 0;
  const imageHeight = meta.height ?? 0;
  if (imageWidth <= 0 || imageHeight <= 0) {
    return { data: input, contentType: "image/jpeg" };
  }

  const shortSide = Math.min(imageWidth, imageHeight);
  const wmWidth = Math.round(shortSide * clamp(cfg.scale_pct ?? 0.18, 0.04, 0.6));
  const opacity = clamp(cfg.opacity ?? 0.45, 0.05, 1);

  let overlay: Buffer;
  if (cfg.mode === "text") {
    overlay = svgText(cfg.text || "Watermark", wmWidth, opacity);
  } else {
    const logoRef = cfg.logo_url || cfg.logo_path;
    if (!logoRef) return { data: input, contentType: "image/jpeg" };
    const logo = await loadAsset(logoRef);
    const trimmedLogo = await trimTransparentPadding(logo);
    overlay = await sharp(trimmedLogo, { failOn: "none" })
      .rotate()
      .resize({ width: wmWidth, withoutEnlargement: true })
      .ensureAlpha()
      .composite([{
        input: Buffer.from([255, 255, 255, Math.round(255 * opacity)]),
        raw: { width: 1, height: 1, channels: 4 },
        tile: true,
        blend: "dest-in",
      }])
      .png()
      .toBuffer();
  }

  const wmMeta = await sharp(overlay).metadata();
  const overlayWidth = wmMeta.width ?? wmWidth;
  const overlayHeight = wmMeta.height ?? wmWidth;
  const centerX = imageWidth * clamp(cfg.x_pct ?? 0.5, 0, 1);
  const centerY = imageHeight * clamp(cfg.y_pct ?? 0.12, 0, 1);
  const left = Math.round(clamp(centerX - overlayWidth / 2, 0, Math.max(0, imageWidth - overlayWidth)));
  const top = Math.round(clamp(centerY - overlayHeight / 2, 0, Math.max(0, imageHeight - overlayHeight)));

  const data = await base
    .composite([{ input: overlay, left, top }])
    .jpeg({ quality: 92, mozjpeg: true })
    .toBuffer();
  return { data, contentType: "image/jpeg" };
}
