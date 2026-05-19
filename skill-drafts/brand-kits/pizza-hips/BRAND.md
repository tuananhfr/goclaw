# Pizza Hip'S Brand Kit

Use this folder as the shared team material source for Pizza Hip'S creative work.

## Required Files

- Headline font: `assets/fonts/SVN-Bango.otf`
- Expected font SHA256: `0C72A0D3D2A61E550E24A35FBF41F0DE3517026F09526F5D8CAF1BBF963FA5D9`
- Machine-readable preset: `render-preset.json`

## Image Workflow

1. Generate or choose a background image with no baked-in text.
2. Leave a clear safe area for headline and CTA text.
3. Render final on-image text with `render_creative`, using the real font file above.
4. Output a flattened PNG/JPG so chat and Discord previews show the full design.
5. Keep `font_sha256` in the result when reporting final output.

## Typography Rules

- Headline/title: SVN - Bango via `assets/fonts/SVN-Bango.otf`.
- Do not claim exact SVN - Bango if the text was only requested in an AI image prompt.
- Keep headline text readable; use stroke/shadow when the background is busy.
- Prefer 1-2 headline lines. Avoid tiny text in the image.

## Brand Visual Rules

- Main color system: orange, blue, black, with white/yellow accents when needed for contrast.
- Keep food/product details visible; do not cover key food, faces, price, hotline, or CTA.
- Top center is reserved for brand/logo watermark when present.
- Bottom right is reserved for hotline/contact/CTA watermark when present.

## Suggested `render_creative` Example

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
  "variants": 3
}
```
