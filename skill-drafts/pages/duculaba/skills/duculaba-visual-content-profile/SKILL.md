---
name: Duculaba Visual Content Profile
description: Page-specific visual profile for Duculaba that maps sales, recipe-support, tips, ingredient knowledge, and lifestyle routes to the right image style, subject focus, text density, and real-photo vs AI usage rules.
---

# Duculaba Visual Content Profile

Use this skill as the page-specific visual profile for `Duculaba - The gioi do lam banh`.

This is mainly used by:

- `Page Manager`
- `Visual Creative Agent`
- `Content Writer Agent`
- `Brand QA / Claim Safety Agent`

The Page Manager owns this profile and passes the relevant part into each task packet.
This profile is a page-specific default map, not a fixed style lock for every post.
If a campaign or brief needs another valid execution, the Manager may override the default mapping in the task packet.

## Core Principle

Duculaba is a sales-first page.
Its visuals should help people:

- recognize the product
- understand the application
- trust the page more
- ask about the right ingredient

Helpful content is allowed, but the visuals should not drift into generic recipe-blog or decorative lifestyle execution.

## Global Visual Direction

- tone: practical, bright, clear, trustworthy, friendly
- visual mood: useful more than flashy
- prioritize readability on mobile
- keep layouts clean and easy to scan
- avoid cluttered promo noise by default
- do not make baking or drink visuals look plastic, over-glossy, or fake
- do not use AI product packaging or AI labels for a real product-specific sales post

## Visual Route Mapping

### 1. Product Sales Posts

Use visual mode:

- `Product Sales Visual`

This is the most important visual route for Duculaba.

Primary rule:

- prioritize real product photos whenever a specific product is being sold

Style:

- product-forward
- clean
- trustworthy
- sales-capable without looking like a loud discount banner

Subject focus:

- real product packaging
- real labels
- real ingredient texture
- real finished product only when it supports the product clearly

Text density:

- low to medium

On-image text:

- 1 clear product name or product-group name
- 1 short application line or key-use point
- optionally 1 short support point

Avoid:

- AI-generated fake packaging as the hero
- putting too many claims on the image
- oversized CTA blocks
- cluttered sale-banner composition

### 2. Recipe Support Posts

Use visual mode:

- `Story Or Experience Visual`
- or a simplified `Educational Infographic`

Style:

- appetizing
- easy to understand
- connected to a real kitchen or serving context

Subject focus:

- finished dish or drink
- ingredient context
- one clear link back to the product or product group

Text density:

- medium

On-image text:

- 1 short dish name or hook
- 1 short supporting line
- optional small note that points toward the ingredient route

Avoid:

- posting recipe visuals that look completely disconnected from what Duculaba sells
- heavy text blocks that read like a recipe sheet

### 3. Tips And Troubleshooting Posts

Use visual mode:

- `Educational Infographic`
- or `Comparison Explainer`

Style:

- clear
- practical
- problem-solving

Subject focus:

- common mistake vs correct handling
- one baking or drink-making issue at a time
- simple ingredient or process illustration

Text density:

- medium

On-image text:

- 1 strong problem-led headline
- 2-4 short support points, reasons, or do/don't cues

Avoid:

- vague decorative visuals with no learning value
- tiny unreadable detail blocks

### 4. Ingredient Knowledge Posts

Use visual mode:

- `Educational Infographic`

Style:

- simple
- credible
- ingredient-led

Subject focus:

- ingredient comparison
- selection guidance
- storage basics
- correct use-case matching

Text density:

- medium

On-image text:

- 1 clear headline
- 2-4 short points or comparison cues

Avoid:

- pretending to show unsupported technical data
- visually implying certification or origin claims that were not provided

### 5. Lifestyle / Soft Community Posts

Use visual mode:

- `Story Or Experience Visual`

Style:

- warm
- close
- natural

Subject focus:

- home kitchen scenes
- small bakery preparation
- drink-making moments
- real ingredient-use atmosphere

Text density:

- low

On-image text:

- 1 short hook at most

Avoid:

- turning the page into a pure inspiration feed
- mood-only visuals with no product or usage connection

## Real Photo vs AI Rule

Use real photography when:

- the post sells a specific product
- the packaging matters
- label trust matters
- the post depends on product authenticity

AI is acceptable when:

- illustrating recipes, tips, or general ingredient knowledge
- building infographic layouts
- creating supporting backgrounds or step illustrations

AI is not acceptable as the main hero when:

- it may misrepresent a real product package
- it may create false labels, false ingredients, or false packaging details

## Text Density Rules

- low density: lifestyle, simple recipe hook, product hero with one key-use line
- medium density: tips, ingredient knowledge, recipe-support explainer
- high density: use only when the Manager explicitly wants infographic-heavy treatment

If the visual uses more text, hierarchy must stay very clear.
Do not turn the image into a paragraph board.

## CTA Treatment On Image

- product sales visuals may include a light CTA, but it is not required
- recipe, tips, and ingredient knowledge visuals should usually keep the stronger CTA in the caption
- do not overload the image with hotline, inbox prompts, and extra CTA blocks at the same time

## Watermark And Safe Zones

Follow the brand skill and current watermark config.

Fallback rule:

- keep the top-left and bottom-centre zones clean for the watermark

Do not place these in the top-left watermark zone:

- headline
- product name
- CTA
- QR code
- small readable detail
- key packaging information

Do not place these in the bottom-centre watermark zone:

- CTA
- hotline
- footer-like text
- product name
- QR code
- small readable detail

## Quality Filters

Before approving a Duculaba visual, verify:

- the chosen visual mode matches the content route
- the image helps sales directly or indirectly
- specific-product sales posts use real product visuals whenever possible
- the text amount fits the route
- the image is easy to read on mobile
- the top-left and bottom-centre watermark zones remain clean
- no false packaging, false labels, or unsupported claims are implied visually

## Final Rule

For Duculaba, a good visual should make the post route obvious at a glance and still support the page's sales-first mission.
