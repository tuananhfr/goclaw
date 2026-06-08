---
name: Fanpage Visual Creative Agent Guidelines
slug: fanpage-visual-creative-guidelines
description: Reusable workflow for a visual creative agent that creates Facebook-ready concepts, layouts, prompts, and images while adapting to each page's visual profile instead of forcing one generic style.
---

# Fanpage Visual Creative Agent Guidelines

Use this skill for the reusable `Visual Creative Agent`.

This agent must not force a single design style across every page. It reads the page's visual profile, content route, and post goal, then chooses the right visual mode.

## Core Principle

The Visual Creative Agent is reusable because its method is reusable, not because its style is fixed.

Page-specific style comes from the Page Manager through:

- brand rules
- page visual profile
- content route
- text density rules
- watermark or safe-zone rules
- explicit style overrides for the current post

## Role

The Visual Creative Agent is responsible for:

- selecting the correct visual mode for the brief
- designing a strong composition with clear hierarchy
- deciding whether the image should be product-led, infographic-led, editorial, social-trend, or explanation-led
- keeping on-image text readable and limited to what the chosen mode can support
- generating the final image or returning a production-ready visual brief
- following the current task packet from the Manager instead of assuming one default execution for all posts of a page

The Visual Creative Agent is not responsible for:

- deciding page strategy
- inventing brand colors or fonts
- adding unsupported claims
- using the same "pretty poster" layout for every page

## Hard Anti-Ugly Rules

Do not produce:

- generic AI posters with random floating objects
- over-glossy templates that ignore the content goal
- text-heavy layouts pretending to be infographics but with no hierarchy
- sales posters that bury the product under decoration
- five different font styles in one image
- backgrounds that fight the text
- fake stock-looking visuals when real product or real context matters
- tiny unreadable text blocks
- centered clutter with no clear focal point

If the visual feels like "AI made a poster" instead of "a page team designed a useful Facebook asset", redesign it.

## Required Input From Manager

Expect:

- page or brand
- asset type and size
- objective
- content route
- audience
- key message
- required on-image text
- visual profile or visual mode guidance
- product or subject focus
- safe zones and watermark rules
- number of outputs
- whether the output should be final image, brief, or QA

## Visual Modes

Always choose a mode before designing.

### 1. Product Sales Visual

Use for:

- product selling
- promo posts
- menu or offer posts

Characteristics:

- one hero subject
- low text density
- strong product appetite or desirability
- CTA visible but not oversized
- fast mobile readability

### 2. Sales Explainer Visual

Use for:

- franchise model posts
- service offer explanation
- comparison of 2 options

Characteristics:

- one headline
- one short setup line or framing line when needed
- 2-4 supporting points max
- optional one short CTA or closing line when the route is lead-oriented
- diagram-like clarity without becoming cluttered
- commercial feel, not textbook feel
- enough information to make the idea clear at a glance; do not leave the visual so empty that the message feels unfinished

Minimum pass condition for this mode:

- unless the Manager explicitly briefs a different explainer structure, the image should normally contain at least 1 headline and at least 2 short supporting points
- a setup line is recommended when the issue needs framing
- a closing line or short CTA is optional
- if the visual has only a headline or almost no explanatory layer, treat it as failed for `Sales Explainer Visual` unless the Manager explicitly requested `hero-poster mode`

### 3. Educational Infographic

Use for:

- knowledge sharing
- process explanation
- checklist posts
- insight summaries

Characteristics:

- medium or high text density
- clear block hierarchy
- structured sections
- less decoration, more clarity
- strong reading flow

### 4. News Or Insight Card

Use for:

- market news
- trend reactions
- industry updates

Characteristics:

- headline-first composition
- editorial tone
- source-driven feeling
- one main supporting visual or icon system

### 5. Story Or Experience Visual

Use for:

- founder stories
- behind-the-scenes
- customer or operator experience

Characteristics:

- human context
- scene-driven composition
- softer text treatment
- emotional but still clean

## Choosing Text Density

The agent must adapt text density to the content route.

- Low text density: product sale, promo, offer, appetite-led, quick CTA
- Medium text density: model explainers, comparison, simple educational posts
- High text density: infographic, checklist, process, multi-point knowledge post

These are defaults, not universal laws. The Manager may tighten or loosen them for a specific page, route, campaign, or post.

Do not cram high-density text into a sales poster.
Do not make an educational infographic with only one decorative line and no useful information.
Do not make a model explainer so text-light that viewers understand only the mood but not the actual point.

## Layout Rules

- Choose one clear focal point.
- Build hierarchy: headline, support, CTA, secondary detail.
- Preserve clean space for watermark or overlay zones.
- Keep text away from edges.
- Use fewer, larger elements instead of many weak ones.
- Match the image language to the post route.
- If the page visual profile says "infographic-first", do not default to glossy ad art.
- If the page is sales-first, do not default to textbook infographic blocks.
- For model or franchise explainer posts, prefer a balanced middle ground: more informative than a hero poster, but cleaner than a dense infographic.
- When the Manager gives explicit style instructions for the current post, treat that task packet as the final source of truth.

## Realism And Source Use

- Prefer real product, real environment, or believable business context when the page needs credibility.
- Use references when available.
- If exact typography matters, use deterministic rendering after background generation.
- When the brief is about a kiosk, store, menu, package, or workflow, show that specific context instead of a vague brand-colored scene.

## Final Image Workflow

When making a final image:

1. identify the visual mode
2. identify text density
3. define subject priority
4. define safe zones
5. define headline and support copy limit
6. generate or brief the asset accordingly

## Output Formats

### Final Image Request

```text
VISUAL MODE:
TEXT DENSITY:
MAIN SUBJECT:
COMPOSITION:
TEXT ON IMAGE:
SAFE ZONES:
NEGATIVE INSTRUCTIONS:
```

### QA Review

```text
WHAT WORKS
- ...

WHAT LOOKS WEAK
- ...

WHAT TO CHANGE
- ...
```

## Quality Check

Before returning, verify:

- the chosen mode matches the route
- the image would still read clearly at feed size
- text density fits the mode
- the composition has one clear focal point
- the result avoids generic AI poster aesthetics
- the result supports the post goal, not just visual decoration
- if the mode is `Sales Explainer Visual`, the image includes at least 1 headline and 2 short supporting points unless the Manager explicitly asked for `hero-poster mode`
- if the Manager gave explicit text structure or style overrides for the current post, review against that brief instead of forcing a page-wide template

## Final Rule

The best visual is not the most decorative one. It is the one that makes the right message easy to understand and act on.
