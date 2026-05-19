---
name: Fanpage Design Guidelines
slug: fanpage-design-guidelines
description: Quy trình cho agent design fanpage: tạo ảnh trực tiếp khi được yêu cầu, chỉ trả prompt/layout brief khi được chỉ định, và luôn bảo vệ safe zone watermark do Lead cung cấp.
---

# Fanpage Design Guidelines

Use this skill for agents creating final images, image prompts, layout briefs, visual directions, asset QA, image-generation instructions, storyboard frames, or Facebook post design guidance.

This skill is brand-neutral. The design agent must use the Lead's task packet for brand identity, visual style, logo/watermark safe zones, and campaign context.

## Core Rule

Design must protect content clarity and reserved overlay zones.

When the task asks to create, generate, design, make, or produce an image, default to creating the final image directly with the available image-generation tool. Do not stop at returning only a prompt.

Return only a prompt, layout brief, direction, or QA checklist when the user or Lead explicitly asks for that output type, or when the image-generation tool is unavailable.

If the Lead provides watermark or logo safe zones, do not place important text, CTA, price, faces, product details, QR codes, or small legal text in those areas.

Do not instruct tools to add, move, resize, or modify watermark assets unless the Lead explicitly asks. If watermark is handled by system tooling, only reserve clean space for it.

Create and return one final image by default. Generate multiple variants only when the Lead explicitly asks for comparison variants.

When using a two-step flow where `create_image` makes a textless background and another tool renders final typography, call `create_image` with `deliver=false` and attach only the final flattened image.

On-image text must never be clipped by canvas edges. Keep readable padding around text and rerender if any letter is cut off.

On-image text must not touch or overlap logo/watermark overlays. Treat safe zones as hard no-text areas, not suggestions.

Font family instructions are for on-image typography only. They apply to text rendered inside generated images, design briefs, banners, covers, stories, or video frames. Do not apply custom fonts to Facebook caption text.

## Required Input From Lead

Expect:

- Brand/page name.
- Asset type: post, story, cover, album, ad, thumbnail, banner.
- Size/aspect ratio.
- Objective and key message.
- Required on-image text.
- Product/subject focus.
- Visual style, colors, on-image fonts, mood.
- Logo/watermark safe zones.
- Must include / must avoid.
- Number of images/options.
- Output type: final image, prompt, layout brief, QA checklist, or final image request.

If the Lead does not specify output type, treat image creation tasks as final image tasks.

## Facebook Size Reference

Use when the Lead has not specified a size:

- Standard square post: 1080x1080 px.
- Story: 1080x1920 px.
- Fanpage cover: 1656x598 px.
- Group cover: 1640x856 px.
- Link share image: 1200x527 px.
- Event banner: up to 1920x1005 px.

## Layout Rules

- Keep the main subject readable and not cropped.
- Keep headline and CTA readable on mobile.
- Use high contrast between text and background.
- Do not place text too close to edges.
- Apply brand font directions only to on-image text when the brief provides them.
- Use consistent visual direction across one post set.
- Prefer product-specific and brand-specific visuals over generic stock-like visuals.
- Do not include unrelated brand logos.
- If the image is for food/F&B, prioritize appetite appeal, realism, product detail, and clean composition.

## Final Image Creation

When asked to create a final image, build the prompt internally and call the available image-generation tool. The tool prompt should include:

- Image goal and platform use.
- Exact format, size, or aspect ratio.
- Brand colors, mood, style, product, setting, and subject priority.
- On-image typography/font direction for headline and supporting text, if provided.
- Composition, text area, CTA area, and safe zones.
- Exact on-image text if required.
- Negative instructions such as no unrelated logos, no distorted text, no cropped product, no text inside watermark zones.

After the image is created, return the image result and only the short notes needed for review.

## Prompt/Brief Template

Use this template only when explicitly asked for a prompt, layout brief, image direction, or asset instructions instead of a final generated image:

```text
IMAGE GOAL:
[Purpose of image.]

FORMAT:
[Size/aspect ratio/platform.]

VISUAL DIRECTION:
[Brand colors, mood, style, product, setting.]

COMPOSITION:
[Main subject, text area, CTA area, empty/safe zones.]

TEXT ON IMAGE:
[Exact text, if any.]

SAFE ZONES:
[Logo/watermark/overlay areas to keep clear.]

NEGATIVE INSTRUCTIONS:
[No unrelated logos, no distorted text, no cropped product, etc.]
```

## QA Checklist

When reviewing or revising an image:

- Is the format correct?
- Is the product/subject clear?
- Is the brand style followed?
- Are text and CTA readable?
- Are watermark/overlay safe zones clear?
- Are important elements away from edges?
- Are there unrelated logos or confusing brand signals?
- Does the image match the caption/content objective?

## Final Rule

For final image tasks, create the image directly when tooling is available. For prompt/brief tasks, return design instructions in a way another tool or designer can execute directly. If important brand or safe-zone information is missing, ask for it or explicitly leave a placeholder.
