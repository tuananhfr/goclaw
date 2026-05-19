# Pizza Hip'S Brand Kit

Use this folder as the shared team material source for Pizza Hip'S creative work.

## Required Files

- Headline font: `assets/fonts/SVN-Bango.otf`
- Expected font SHA256: `0C72A0D3D2A61E550E24A35FBF41F0DE3517026F09526F5D8CAF1BBF963FA5D9`
- Machine-readable preset: `render-preset.json`

## Image Workflow

1. Generate or choose a background image with no baked-in text.
   - If using `create_image` only as the background before text rendering, call it with `"deliver": false` so the raw background is not attached.
2. Leave clear safe areas for headline/CTA text and for system watermark overlays.
3. Render final on-image text with `render_creative`, using the real font file above.
4. Output a flattened PNG/JPG so chat and Discord previews show the full design.
5. Keep `font_sha256` in the result when reporting final output.
6. Create and attach one final image by default. Do not generate multiple variants unless the Lead explicitly asks for comparison variants.

## Typography Rules

- Headline/title: SVN - Bango via `assets/fonts/SVN-Bango.otf`.
- Do not claim exact SVN - Bango if the text was only requested in an AI image prompt.
- Keep headline text readable; use stroke/shadow when the background is busy.
- Prefer 1-2 headline lines. Avoid tiny text in the image.

## Brand Visual Rules

- Main color system: orange, blue, black, with white/yellow accents when needed for contrast.
- Keep food/product details visible; do not cover key food, faces, price, hotline, or CTA.
- Top center is reserved for brand/logo watermark when present. Keep this zone visually clean; do not place headline text there.
- Bottom right is reserved for hotline/contact/CTA watermark when present. Keep this zone visually clean; do not place headline text, CTA, price, food hero details, QR codes, or small readable text there.
- Treat watermark zones as overlay zones that may be applied after the image is generated. Final art must still look complete after those overlays are added.
- Put headline text in a clean non-watermark area, usually upper-left, upper-right, or center-top only when the top-center logo zone remains clear.

## Suggested `render_creative` Example

```json
{
  "tool": "create_image",
  "prompt": "Pizza product background, no text, leave clean non-watermark text area...",
  "aspect_ratio": "1:1",
  "filename_hint": "pizza-hips-bg",
  "deliver": false
}
```

Then render the final attached image:

```json
{
  "base_image_path": "page1-bg.png",
  "output_path": "page1-final.png",
  "font_path": "brand-kits/pizza-hips/assets/fonts/SVN-Bango.otf",
  "texts": [
    {
      "text": "PIZZA HIPS",
      "layout": "auto",
      "size": 96,
      "color": "#FFD84A",
      "stroke_color": "#E53935",
      "stroke_width": 3,
      "max_width": 820
    }
  ],
  "variants": 1
}
```
