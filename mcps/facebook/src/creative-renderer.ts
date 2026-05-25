import crypto from "crypto";
import sharp from "sharp";
import * as opentype from "opentype.js";

export interface TextLayerInput {
  text: string;
  font_path: string;
  font_size: number;
  color: string;
  x_pct: number;
  y_pct: number;
  max_width_pct?: number;
  align?: "left" | "center" | "right";
  line_height?: number;
  letter_spacing?: number;
  opacity?: number;
}

export interface RenderCreativeInput {
  canvas_width: number;
  canvas_height: number;
  background_color?: string;
  background_image?: Buffer;
  text_layers: TextLayerInput[];
  output_format?: "png" | "jpeg";
}

export interface FontAssetLoader {
  (fontPath: string): Promise<Buffer>;
}

export interface RenderCreativeResult {
  data: Buffer;
  contentType: string;
  fontMeta: Array<{
    font_path: string;
    font_sha256: string;
    font_family_from_file?: string;
  }>;
}

type Font = opentype.Font & {
  unitsPerEm: number;
  ascender: number;
  descender: number;
  names?: { fontFamily?: Record<string, string> };
  stringToGlyphs(text: string): opentype.Glyph[];
  getKerningValue(left: opentype.Glyph, right: opentype.Glyph): number;
};

interface LaidOutLine {
  text: string;
  width: number;
}

export async function renderCreative(input: RenderCreativeInput, loadFont: FontAssetLoader): Promise<RenderCreativeResult> {
  const width = input.canvas_width;
  const height = input.canvas_height;
  const outputFormat = input.output_format ?? "png";
  const base = input.background_image
    ? sharp(input.background_image).resize(width, height, { fit: "cover" }).toColourspace("srgb")
    : sharp({
        create: {
          width,
          height,
          channels: 4,
          background: input.background_color ?? "#ffffff",
        },
      });

  const fontCache = new Map<string, { font: Font; buffer: Buffer; sha256: string }>();
  const fontMeta = new Map<string, { font_path: string; font_sha256: string; font_family_from_file?: string }>();

  const layerSvgs: Buffer[] = [];
  for (const layer of input.text_layers) {
    const fontAsset = await loadCachedFont(layer.font_path, loadFont, fontCache);
    const family = fontAsset.font.names?.fontFamily?.en;
    fontMeta.set(layer.font_path, {
      font_path: layer.font_path,
      font_sha256: fontAsset.sha256,
      font_family_from_file: family,
    });
    layerSvgs.push(renderTextLayerSvg(layer, fontAsset.font, width, height));
  }

  const data = await base
    .composite(layerSvgs.map((inputSvg) => ({ input: inputSvg, left: 0, top: 0 })))
    .toFormat(outputFormat, outputFormat === "jpeg" ? { quality: 92 } : undefined)
    .toBuffer();

  return {
    data,
    contentType: outputFormat === "jpeg" ? "image/jpeg" : "image/png",
    fontMeta: Array.from(fontMeta.values()),
  };
}

async function loadCachedFont(
  fontPath: string,
  loadFont: FontAssetLoader,
  cache: Map<string, { font: Font; buffer: Buffer; sha256: string }>,
): Promise<{ font: Font; buffer: Buffer; sha256: string }> {
  const cached = cache.get(fontPath);
  if (cached) return cached;
  const buffer = await loadFont(fontPath);
  const arrayBuffer = new Uint8Array(buffer).buffer;
  const font = opentype.parse(arrayBuffer) as Font;
  const sha256 = crypto.createHash("sha256").update(buffer).digest("hex");
  const loaded = { font, buffer, sha256 };
  cache.set(fontPath, loaded);
  return loaded;
}

function renderTextLayerSvg(layer: TextLayerInput, font: Font, canvasWidth: number, canvasHeight: number): Buffer {
  const fontSize = layer.font_size;
  const lineHeightPx = fontSize * (layer.line_height ?? 1.15);
  const maxWidth = layer.max_width_pct ? canvasWidth * (layer.max_width_pct / 100) : undefined;
  const lines = layoutLines(layer.text, font, fontSize, layer.letter_spacing ?? 0, maxWidth);
  const x = canvasWidth * (layer.x_pct / 100);
  const top = canvasHeight * (layer.y_pct / 100);
  const scale = fontSize / font.unitsPerEm;
  const baselineOffset = font.ascender * scale;
  const opacity = normalizeOpacity(layer.opacity);
  const align = layer.align ?? "left";

  const paths: string[] = [];
  lines.forEach((line, index) => {
    const baselineY = top + baselineOffset + index * lineHeightPx;
    let lineX = x;
    if (align === "center") lineX = x - line.width / 2;
    if (align === "right") lineX = x - line.width;
    paths.push(textToPathData(font, line.text, lineX, baselineY, fontSize, layer.letter_spacing ?? 0));
  });

  const fill = sanitizeColor(layer.color);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${canvasWidth}" height="${canvasHeight}" viewBox="0 0 ${canvasWidth} ${canvasHeight}">
<g fill="${fill}" opacity="${opacity}">${paths.map((d) => `<path d="${d}"/>`).join("")}</g>
</svg>`;
  return Buffer.from(svg);
}

function layoutLines(text: string, font: Font, fontSize: number, letterSpacing: number, maxWidth?: number): LaidOutLine[] {
  if (!maxWidth || maxWidth <= 0) {
    return text.split(/\r?\n/).map((line) => ({ text: line, width: measureText(font, line, fontSize, letterSpacing) }));
  }
  const lines: LaidOutLine[] = [];
  for (const paragraph of text.split(/\r?\n/)) {
    const words = paragraph.trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) {
      lines.push({ text: "", width: 0 });
      continue;
    }
    let current = "";
    for (const word of words) {
      const candidate = current ? `${current} ${word}` : word;
      const candidateWidth = measureText(font, candidate, fontSize, letterSpacing);
      if (current && candidateWidth > maxWidth) {
        lines.push({ text: current, width: measureText(font, current, fontSize, letterSpacing) });
        current = word;
      } else {
        current = candidate;
      }
    }
    if (current) lines.push({ text: current, width: measureText(font, current, fontSize, letterSpacing) });
  }
  return lines;
}

function measureText(font: Font, text: string, fontSize: number, letterSpacing: number): number {
  const glyphs = font.stringToGlyphs(text);
  const scale = fontSize / font.unitsPerEm;
  let width = 0;
  for (let i = 0; i < glyphs.length; i++) {
    const glyph = glyphs[i];
    if (i > 0) width += font.getKerningValue(glyphs[i - 1], glyph) * scale;
    width += (glyph.advanceWidth ?? 0) * scale;
    if (i < glyphs.length - 1) width += letterSpacing;
  }
  return width;
}

function textToPathData(font: Font, text: string, x: number, baselineY: number, fontSize: number, letterSpacing: number): string {
  const glyphs = font.stringToGlyphs(text);
  const scale = fontSize / font.unitsPerEm;
  let cursorX = x;
  const data: string[] = [];
  for (let i = 0; i < glyphs.length; i++) {
    const glyph = glyphs[i];
    if (i > 0) cursorX += font.getKerningValue(glyphs[i - 1], glyph) * scale;
    const path = glyph.getPath(cursorX, baselineY, fontSize);
    data.push(path.toPathData(2));
    cursorX += (glyph.advanceWidth ?? 0) * scale + letterSpacing;
  }
  return data.join("");
}

function normalizeOpacity(value?: number): number {
  if (value === undefined) return 1;
  const normalized = value > 1 ? value / 100 : value;
  return Math.max(0, Math.min(1, normalized));
}

function sanitizeColor(value: string): string {
  const trimmed = value.trim();
  if (/^#[0-9a-fA-F]{3,8}$/.test(trimmed)) return trimmed;
  if (/^rgba?\(\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}(?:\s*,\s*(?:0|1|0?\.\d+))?\s*\)$/.test(trimmed)) return trimmed;
  throw new Error(`Unsupported color value: ${value}`);
}
