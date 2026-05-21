---
name: Facebook Fanpage Content Guidelines
slug: facebook-fanpage-content-guidelines
description: Quy chuẩn chung cho agent khi viết content, tạo brief hình ảnh, kiểm tra bài đăng và chuẩn bị nội dung cho Facebook fanpage.
---

# Facebook Fanpage Content Guidelines

Use this skill whenever the task involves Facebook fanpage content, social posts, ad copy, post captions, content calendars, image briefs, or visual QA for fanpage assets.

## Output Checklist

Before returning any Facebook fanpage content, verify:

- The post has a clear title or opening hook.
- The body states the customer problem, benefit, difference, or offer clearly.
- The post has a call to action.
- Footer/footage is present when project information is available.
- Hashtags are separated from footer by one blank line.
- The first hashtag is the brand hashtag when a brand hashtag is known.
- Image brief includes current logo/watermark config or fallback placement when images are requested.
- No unsupported claim, wrong technical term, or unrelated brand logo is introduced.

## Post Structure

### Length And Local Context

- Keep normal Facebook post copy under 400 words unless the user explicitly asks for a long-form post.
- Prefer short paragraphs: 1-3 short sentences per paragraph.
- Use short, direct sentences that are easy to scan on mobile.
- Write like a real page admin posting to Facebook: natural, specific, and lightly conversational. Avoid sounding like an essay, report, or generic AI-generated marketing text.
- Do not use visible framework labels such as "AIDA", "PAS", "Problem", "Solution", "CTA", or "Hook" in publish-ready copy unless the user asks for an analysis draft.
- Avoid long introduction blocks. Make the first 2-3 lines strong enough to work above the Facebook "see more" fold.
- When the post is tied to a location, event, store area, or local audience, include the relevant area/region in the copy.
- When weather, season, heat, rain, cold, holidays, or local timing affects the angle, include weather or local-context data if provided by the Lead or verified by research/tools. Do not invent current weather.

### Title

- Keep it short, clear, and attention-grabbing.
- Focus on the main benefit, offer, event, or customer pain point.
- Avoid font-transform text for ad copy because transformed fonts can break display or ad review.
- Use uppercase sparingly for emphasis.

### Caption Font Scope

- Facebook captions are plain text. Do not promise or request custom font families such as SVN Bango, SVN Avo, Montserrat, or similar for caption text.
- Brand font rules apply to on-image text inside creative assets only, such as headlines, offer text, CTA labels, price text, or small notes rendered into an image/video.

### Body

- Start from customer insight, pain point, desire, or occasion.
- Keep paragraphs short and separated by line breaks.
- Explain value, benefits, improvements, differences, or reasons to choose the product/service.
- Use precise terms for the industry. Do not invent or misuse technical terms.
- Keep wording concise and easy to scan on mobile.

### Call To Action

Every post must include a CTA. Match the CTA to the goal:

- Inbox/DM for consultation.
- Call hotline for booking or ordering.
- Visit store/location.
- Register, reserve, order, comment, or follow.
- End the publish copy with one provocative or open-ended question when it fits the post goal. The question should invite comment, inbox, or self-reflection.

### Footer / Footage

If project footer information is provided, use it exactly and do not invent new footer details.

Expected footer fields when available:

- Project or brand name
- Address
- Hotline
- Website or landing page

### Hashtags

- Use at most 5 hashtags for normal posts unless the project requests otherwise.
- Pick the 3-5 most accurate hashtags. Relevance beats quantity.
- Mix fixed brand hashtags with contextual hashtags, but do not pad the post with weak generic tags.
- The first hashtag should be the brand hashtag when known.
- Put hashtags after the footer, separated by a blank line.
- Do not overuse generic hashtags that do not match the post.

### Icons

- Use icons in the title or at the start of short content sections when they make the post easier to scan.
- Prefer icons that match the meaning of the line.
- Red/yellow icons can be used for attention, offers, urgency, or food content.
- Put one space between icon and text.
- Use icons moderately; avoid clutter in short posts.
- For list-style captions, use icon bullets to create clear scan points when they improve readability.
- Do not put icons on every line. A normal Facebook post usually needs 1 title icon and 2-4 useful scan-point icons at most.

## Image Guidelines

Use these rules when creating image prompts, image briefs, or reviewing generated/social images:

- Image and caption must support the same message.
- Prefer real, product-specific, or brand-specific visuals over generic stock-like images.
- Images must be clear, sharp, and not crop important text, product, logo, or CTA.
- Images must include the brand logo or watermark when brand assets are available.
- When a runtime watermark system is available, use the current watermark config as the source of truth for reserved overlay space. Do not rely on a hardcoded safe zone unless the config cannot be fetched.
- Do not use images containing another brand's logo unless the source/permission is explicitly required and mentioned.
- Keep layout readable on mobile.
- For product posts, prioritize product realism and appetizing/usable detail.
- Do not place text too close to image edges.
- Do not place important text, CTA, QR codes, faces, product details, or small legal text inside configured watermark overlay zones.

## Visual Layout Heuristics

- For post images, the main subject should usually occupy about two-thirds of the layout.
- Leave supporting text, CTA, or brand elements in the remaining visual area.
- Use one coherent background/color direction per post or album. Do not mix unrelated background systems in the same post set.
- Make sure text contrast is high and typography is readable at feed size.
- Treat font family instructions as on-image typography instructions only.
- For two-step images, generate a textless background first, then render final typography with `render_creative`; pass the current watermark config into `render_creative.watermark` when available.

## Facebook Image Sizes

Use these common target sizes when the user asks for a Facebook asset:

- Avatar: 2048x2048 px preferred, minimum 168x168 px.
- Fanpage cover: 1656x598 px.
- Group cover: 1640x856 px.
- Link share image: 1200x527 px.
- Event banner: up to 1920x1005 px.
- Standard square product/post image: 1080x1080 px.
- Facebook story: 1080x1920 px.

Album guidance:

- 4 vertical images: first 603x900 px, remaining 900x900 px.
- 4 horizontal images: first 900x603 px, remaining 900x900 px.
- 4 square images: 900x900 px.
- 3 horizontal images: first 900x452 px, remaining 900x900 px.
- 3 vertical images: first 448x900 px, remaining 900x900 px.
- 2 horizontal images: 900x452 px each.
- 2 vertical images: 448x900 px each.

## When Project Rules Exist

If project, brand, or domain-specific skills are available, apply them together:

1. This general Facebook fanpage skill.
2. The project/brand-specific skill for identity, tone, footer, hashtag, and assets.
3. The domain-specific skill for subject matter such as franchise, legal, finance, medicine, technology, or industry-specific claims.

When rules conflict, project/domain-specific rules override general formatting guidance, except for safety, truthfulness, and platform suitability.
