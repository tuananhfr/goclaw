---
name: Brand Ki Su San Phang Guidelines
description: Brand, Vietnamese content, visual, hashtag, footer, watermark, and page-positioning rules for the Kĩ sư sàn phẳng Facebook page. Use for Vietnamese technical posts, image briefs, content calendars, lead-neutral knowledge posts, and review of flat slab, UBoot Beton, construction standard, and structural engineering content.
---

# Brand Kĩ Sư Sàn Phẳng Guidelines

Use this skill for any task involving the Kĩ sư sàn phẳng page, Vietnamese technical construction content, flat slab knowledge, UBoot Beton, structural engineering experience, or construction-standard comparison content published under this page.

Also apply the general `facebook-fanpage-baseline-rules` skill when available. For technical topics, also apply `ki-su-san-phang-structural-knowledge` when available.

This brand skill controls page positioning, Vietnamese voice, footer, watermark safe zone, hashtag selection, visual direction, and publishing checks.

## Page Basics

- Page name: Kĩ sư sàn phẳng.
- Language: Vietnamese.
- Page type: technical knowledge channel, not a hard-selling company page.
- Main content focus: flat slab systems, UBoot Beton, structural engineering, construction standards, global standard comparison, complex structural design, and practical engineering experience.
- Company relationship: LPC may be introduced as the organization behind the channel, but the content should mainly educate and discuss technical knowledge.
- Watermark safe zone: use the current page watermark config when available. Fallback: top left. Keep this area visually clean.
- Audience: engineers, architects, construction professionals, contractors, developers, students, and technically curious homeowners.

## Brand Font Assets

Use the Kĩ sư sàn phẳng brand kit when exact Vietnamese on-image typography is needed:

- Brand kit workspace path: `brand-kits/ki-su-san-phang`.
- Headline/title font: Be Vietnam Pro ExtraBold, `brand-kits/ki-su-san-phang/assets/fonts/BeVietnamPro-ExtraBold.ttf`.
- Emphasis font: Be Vietnam Pro Bold, `brand-kits/ki-su-san-phang/assets/fonts/BeVietnamPro-Bold.ttf`.
- Body/supporting text font: Be Vietnam Pro Medium, `brand-kits/ki-su-san-phang/assets/fonts/BeVietnamPro-Medium.ttf`.
- Small note/source font: Be Vietnam Pro Regular, `brand-kits/ki-su-san-phang/assets/fonts/BeVietnamPro-Regular.ttf`.

Font rules apply only to text rendered inside images/videos/creative assets. Facebook caption text remains plain text.

Do not claim exact Be Vietnam Pro rendering if the image was produced only by an AI image prompt. Use `render_creative` or an equivalent deterministic font-render tool with the real font files for exact Vietnamese typography.

Mandatory image workflow:

1. Use `create_image` only for a textless technical background. The prompt must explicitly say: no text, no letters, no numbers, no readable labels, no UI text, no typography.
2. When `create_image` is only an intermediate background, call it with `deliver=false`.
3. Before rendering final text, get the current page watermark config when an `fb_get_watermark_config` tool is available.
4. Render all final Vietnamese on-image text with `render_creative` using the Be Vietnam Pro font paths above. Pass the watermark config into `render_creative.watermark` so text avoids the real configured watermark position.
5. If the watermark config cannot be fetched, keep the top-left watermark zone clean manually and state that the exact watermark config was unavailable.
6. Attach or return only the final flattened image from `render_creative`, not the raw AI background.
7. If rendering more than one text layer, set explicit `x`, `y`, `align`, `size`, and `max_width` for each layer, or rely on the tool's auto-layout to place each layer in a separate zone. Never place headline and subtitle at the same coordinates.

On-image Vietnamese text must never be clipped. Keep enough top padding for accents and rerender if any dấu is cut off.

## Channel Objective

Use this page to:

- Build a practical Vietnamese knowledge channel about flat slabs and structural engineering.
- Explain construction standards around the world and compare them with Vietnam when useful.
- Share engineering experience, design lessons, technical mistakes, and case-style observations.
- Educate the audience about UBoot Beton, flat slab, long-span slab, hollow slab, green materials, ESG, and construction material choices.
- Build trust for LPC through expertise, not through aggressive sales language.

Do not turn every post into a company introduction. Mention LPC only where it helps establish source, footer, or technical credibility.

## Page Voice

Write in a voice that is:

- Vietnamese-first, clear, and technically credible.
- Practical and experience-based.
- Easy to read for engineers and construction decision makers.
- Cautious when discussing standards, structural performance, legal compliance, or project-specific design.
- Professional but not overly academic.

Avoid:

- Generic motivational construction content.
- Over-selling LPC or making every post a service pitch.
- Unsupported claims about savings, span, load, safety, ESG, approval, or code compliance.
- Writing like a textbook without practical takeaways.

## Post Format

For normal publish-ready posts, use this structure:

```text
[TITLE IN VIETNAMESE]

[Opening hook: practical technical question, misconception, or project situation.]

[Short explanation or analysis.]

[Practical takeaway / checklist / engineering lesson.]

[Soft CTA if suitable.]

[Fixed footer]

[Hashtags]
```

Rules:

- Keep normal Facebook post copy under 400 words unless the user asks for long-form.
- Use short paragraphs that are easy to scan on mobile.
- Use icons moderately when they improve scanability.
- Do not show framework labels such as AIDA, PAS, Problem, Solution, or CTA in publish-ready copy.
- End with a question when it naturally encourages discussion from engineers or project owners.
- For highly technical topics, include a short caution that project-specific design must be checked by qualified engineers and applicable standards.

## Content Pillars

Use these routes:

- Construction standards: Eurocode, ACI, BS, AS/NZS, Japanese/Korean/Singapore references, Vietnamese standards, and practical comparison when sourced.
- Flat slab knowledge: sàn phẳng, sàn không dầm, sàn vượt nhịp, sàn rỗng, UBoot Beton, punching shear, deflection, vibration, MEP coordination, slab depth, and construction workflow.
- Complex structural design: basements, transfer floors, long spans, irregular geometry, high-rise constraints, large openings, heavy loads, seismic/wind considerations, and coordination risks.
- Engineering experience: design mistakes, site coordination issues, drawing checklist, model/detail coordination, lessons from real projects, and decision questions before choosing a system.
- Green construction: material efficiency, ESG discussion, reduced material use, waste reduction, documentation, and responsible technical claims.

## Footer

Use this fixed footer by default:

```text
CÔNG TY TNHH XÂY DỰNG LÂM PHẠM - LPC
◼️ Địa chỉ: 226 Lê Trọng Tấn, phường Phương Liệt, Hà Nội
◼️ Hotline: +84911.29.96.96
◼️ Website: https://lpc.vn
◼️ Email: info@lpc.vn
```

Use the footer exactly unless the user provides an updated version. Do not invent alternate phone numbers, emails, websites, addresses, or office names.

## Hashtags

Use at most 5-8 hashtags by default. Prefer relevance over volume.

Default pool:

`#LPC #sanphangubot #Sanhop #UBootBeton #vatlieuxanh #ESG #hopubot #construction #Sanphangkhongdam #sanvuotnhip #vatlieuxaydung #xaydungnhadan`

Selection guidance:

- Flat slab / UBoot post: choose from `#LPC`, `#sanphangubot`, `#UBootBeton`, `#Sanphangkhongdam`, `#sanvuotnhip`, `#Sanhop`.
- Green material / ESG post: choose from `#LPC`, `#vatlieuxanh`, `#ESG`, `#vatlieuxaydung`, `#construction`.
- Home construction / practical knowledge post: choose from `#LPC`, `#construction`, `#xaydungnhadan`, `#vatlieuxaydung`.
- First hashtag should usually be `#LPC`.
- Hashtags can be customized by topic. Do not use the full pool unless requested.

## Image Rules

For image prompts, briefs, or QA:

- Do not ask `create_image` to render readable Vietnamese text, headlines, captions, labels, tables, standards, or CTA copy. AI image generation must create background/visual material only.
- Any final feed image with Vietnamese text must be a two-step output: textless background from `create_image` plus final typography from `render_creative`.
- Watermark is handled by the existing system/tooling. Do not instruct the agent to add, move, resize, or modify watermark assets.
- Use the current page watermark config as the source of truth when available, and pass it into `render_creative.watermark` during final text rendering.
- If current watermark config is unavailable, use the fallback top-left safe zone.
- Do not place headline text, CTA, small technical notes, project details, drawings, faces, QR codes, or key structural diagrams in the configured watermark zone.
- Prefer realistic technical visuals: structural drawings, flat slab diagrams, BIM/model views, site details, slab reinforcement, UBoot module illustrations, standard-comparison layouts, or clean engineering infographics.
- Keep visual tone technical and educational, not consumer-advertising-heavy.
- Keep text readable on mobile.
- Use Be Vietnam Pro from the brand kit for final on-image Vietnamese text when exact rendering is needed.
- Avoid showing confidential project identifiers or unrelated brand logos.
- For standard square posts, use 1080x1080 px unless the user specifies another size.

## CTA Guidance

Use soft CTAs because this is a knowledge channel:

- "Anh em kỹ sư từng gặp tình huống này chưa?"
- "Bạn muốn phân tích sâu hơn tiêu chuẩn nào?"
- "Cần trao đổi thêm về giải pháp sàn phẳng, có thể liên hệ LPC để được tư vấn kỹ thuật."
- "Theo bạn, khi nào nên cân nhắc sàn phẳng thay vì hệ dầm truyền thống?"

Avoid turning every CTA into a sales pitch.

## Final Self-Check

Before returning Kĩ sư sàn phẳng content, verify:

- The post is in Vietnamese.
- The content behaves like a technical knowledge channel.
- LPC is introduced lightly and mainly through footer or expertise context.
- CTA exists when suitable, but is not overly sales-focused.
- Fixed footer is present unless the user asks to omit it.
- Hashtags fit the topic.
- Current watermark config is protected in image briefs, with top left used only as fallback.
- Technical, legal, code, ESG, cost, safety, or performance claims are cautious and supported when needed.
