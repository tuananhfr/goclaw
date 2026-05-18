---
name: Fanpage Design Guidelines
slug: fanpage-design-guidelines
description: Quy trình cho agent design fanpage: tạo prompt ảnh, layout brief, visual QA và image direction dựa trên brand/page context, kích thước Facebook và safe zone watermark do Lead cung cấp.
---

# Fanpage Design Guidelines

Use this skill for agents creating image prompts, layout briefs, visual directions, asset QA, image-generation instructions, storyboard frames, or Facebook post design guidance.

This skill is brand-neutral. The design agent must use the Lead's task packet for brand identity, visual style, logo/watermark safe zones, and campaign context.

## Core Rule

Design must protect content clarity and reserved overlay zones.

If the Lead provides watermark or logo safe zones, do not place important text, CTA, price, faces, product details, QR codes, or small legal text in those areas.

Do not instruct tools to add, move, resize, or modify watermark assets unless the Lead explicitly asks. If watermark is handled by system tooling, only reserve clean space for it.

## Required Input From Lead

Expect:

- Brand/page name.
- Asset type: post, story, cover, album, ad, thumbnail, banner.
- Size/aspect ratio.
- Objective and key message.
- Required on-image text.
- Product/subject focus.
- Visual style, colors, fonts, mood.
- Logo/watermark safe zones.
- Must include / must avoid.
- Number of images/options.
- Output type: prompt, layout brief, QA checklist, or final image request.

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
- Use consistent visual direction across one post set.
- Prefer product-specific and brand-specific visuals over generic stock-like visuals.
- Do not include unrelated brand logos.
- If the image is for food/F&B, prioritize appetite appeal, realism, product detail, and clean composition.

## Prompt Template

When asked to create an image prompt:

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

Return design instructions in a way another tool or designer can execute directly. If important brand or safe-zone information is missing, ask for it or explicitly leave a placeholder.
