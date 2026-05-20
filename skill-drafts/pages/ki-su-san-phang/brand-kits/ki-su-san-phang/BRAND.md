# Kĩ Sư Sàn Phẳng Brand Kit

Use this folder as the shared team material source for Kĩ sư sàn phẳng creative work.

## Required Files

- Headline font: `assets/fonts/BeVietnamPro-ExtraBold.ttf`
- Body font: `assets/fonts/BeVietnamPro-Medium.ttf`
- Small note/source font: `assets/fonts/BeVietnamPro-Regular.ttf`
- Strong emphasis font: `assets/fonts/BeVietnamPro-Bold.ttf`
- Machine-readable preset: `render-preset.json`

## Font SHA256

- `BeVietnamPro-Regular.ttf`: `CB6F53D6FD56316356C55B067474D9621C27B7182B124063B8F6EAB6ACEF63BD`
- `BeVietnamPro-Medium.ttf`: `F493144CB53C78C6F0819BC9B73DE274486618AC609BC92B3B79D4EFC76F95C6`
- `BeVietnamPro-Bold.ttf`: `2FE6C3F9DBB65F61554D530C16B2EB4EC32CA437DC8F9999A6EC0F6403B30BFE`
- `BeVietnamPro-ExtraBold.ttf`: `10FCB42B9CEE8E30918843416676AE059E8F77FE439D0C3A4350617EF9F77A75`

## Image Workflow

1. Generate or choose a background image with no baked-in text.
   - If using `create_image` only as the background before text rendering, call it with `"deliver": false` so the raw background is not attached.
   - The background prompt must explicitly say: no text, no letters, no numbers, no readable labels, no UI text, no typography.
2. Leave clean space for technical headline/body text and system watermark overlays.
3. Render final Vietnamese text with `render_creative` or an equivalent deterministic font-render tool using the real Be Vietnam Pro font files above.
4. Output a flattened PNG/JPG so previews show the final design.
5. Keep `font_path` and `font_sha256` in result metadata when reporting final output.
6. Create one final image by default. Generate variants only when the Lead asks for comparison options.

## Typography Rules

- Headline/title: Be Vietnam Pro ExtraBold via `assets/fonts/BeVietnamPro-ExtraBold.ttf`.
- Secondary headline or emphasis: Be Vietnam Pro Bold via `assets/fonts/BeVietnamPro-Bold.ttf`.
- Body/supporting text: Be Vietnam Pro Medium via `assets/fonts/BeVietnamPro-Medium.ttf`.
- Source/note/small text: Be Vietnam Pro Regular via `assets/fonts/BeVietnamPro-Regular.ttf`.
- Do not claim exact Be Vietnam Pro rendering if the text was only requested inside an AI image prompt.
- Never ask the image model to render final Vietnamese text directly. Use the image model for textless visuals only, then render final text with `render_creative`.
- Keep Vietnamese diacritics readable; never let accents be clipped at the top.
- Prefer 1-2 headline lines. Use body text sparingly on feed images.
- Keep at least 5% padding around text blocks.

## Brand Visual Rules

- Main color system: technical blue, concrete gray, white, black, with green accents for material-efficiency or ESG topics.
- Keep technical diagrams, drawings, slab details, reinforcement, and UBoot module visuals clear.
- Top left is reserved for the page watermark. Keep this zone visually clean; do not place headline text, CTA, drawing labels, QR codes, or small readable text there.
- Treat the watermark zone as an overlay zone that may be applied after the image is generated.
- Put headline text in a clean non-watermark area, usually upper-right, mid-left, mid-right, or lower-left when the top-left watermark zone remains clear.
- On-image text must never be clipped by the canvas edge. Vietnamese accents must remain fully visible.
- On-image text must not touch or overlap watermark overlays.
- Avoid cluttered technical drawings behind small text. Add a solid or translucent text panel only when needed for readability.

## Suggested `render_creative` Example

```json
{
  "tool": "create_image",
  "prompt": "technical flat slab construction background, BIM model and slab detail, no text, leave clean negative space outside top-left watermark zone...",
  "aspect_ratio": "1:1",
  "filename_hint": "ki-su-san-phang-bg",
  "deliver": false
}
```

Then render the final attached image:

```json
{
  "base_image_path": "page1-bg.png",
  "output_path": "page1-final.png",
  "font_path": "brand-kits/ki-su-san-phang/assets/fonts/BeVietnamPro-ExtraBold.ttf",
  "texts": [
    {
      "text": "SÀN PHẲNG KHÔNG DẦM",
      "x": 820,
      "y": 260,
      "align": "center",
      "size": 78,
      "color": "#FFFFFF",
      "stroke_color": "#0F172A",
      "stroke_width": 2,
      "max_width": 430
    },
    {
      "text": "Những điểm cần kiểm tra trước khi chọn giải pháp",
      "x": 820,
      "y": 455,
      "align": "center",
      "size": 34,
      "color": "#FFFFFF",
      "stroke_color": "#0F172A",
      "stroke_width": 1,
      "max_width": 460
    }
  ],
  "variants": 1
}
```
