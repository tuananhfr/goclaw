---
name: Brand Pizza Hips Guidelines
slug: brand-pizza-hips-guidelines
description: Quy chuẩn nhận diện thương hiệu Pizza Hip'S cho agent khi viết content, lên brief hình ảnh và kiểm tra nội dung Facebook thuộc hệ sinh thái Pizza Hip'S.
---

# Brand Pizza Hip'S Guidelines

Use this skill for any task involving Pizza Hip'S, Hips Resto, or any Facebook page that belongs to the Pizza Hip'S brand ecosystem.

Also apply the general `facebook-fanpage-content-guidelines` skill when available.

For franchise/legal/business-model content, also apply `pizza-hips-franchise-knowledge` when available. This brand skill controls Pizza Hip'S identity, page/channel direction, visual system, wording consistency, watermark, and hashtag conventions.

## Brand Basics

- Brand name: Pizza Hip'S.
- Main visual tone: orange, blue, and black.
- On-image title/headline font direction for creative briefs: SVN - Bango.
- On-image body/supporting text font direction for creative briefs: SVN - Avo.
- Keep visual tone consistent within the same post or album.
- Do not use two unrelated background colors/styles in one image set.

These font directions apply only to text rendered inside images/videos/creative assets. Facebook caption text remains plain text and cannot use custom brand fonts.

## Brand Font Assets

Use these files when exact on-image typography must be rendered or verified:

- Headline/title/display text:
  - Font: SVN - Bango.
  - Font file: `{baseDir}/assets/fonts/SVN-Bango.otf`.
  - Runtime fallback pattern: `<skills.file_path>/assets/fonts/SVN-Bango.otf`, where `skills.file_path` is the active version directory in the skills database.
  - SHA256: `0C72A0D3D2A61E550E24A35FBF41F0DE3517026F09526F5D8CAF1BBF963FA5D9`.

If `{baseDir}` is not resolved by the current tool/agent context, query the active skill metadata and use `<skills.file_path>/assets/fonts/SVN-Bango.otf`. Do not guess a hardcoded version number. Do not search parent directories with `../` to find this font. If the font path cannot be read, report the exact path access error instead of switching to a fallback font.

- Body/supporting text:
  - Font direction: SVN - Avo.
  - Exact font file is not provided yet. Do not claim exact SVN - Avo rendering until the font file is available.

If a final creative must be confirmed as brand-font accurate, do not rely on AI image generation prompts alone. Use the actual font file above in a render/edit tool, or require editable source confirmation from the designer.

Exact font workflow:

- When the user asks for exact SVN - Bango on-image text, use `fb_render_creative` or an equivalent deterministic font-render tool with the real `SVN-Bango.otf` file path.
- Do not claim exact SVN - Bango if the image was produced only by `create_image` or an AI image prompt.
- If the render tool cannot read the font path, report the tool/path access error clearly. Do not silently switch to "near SVN - Bango" or "similar display rounded font".
- After rendering, keep the returned `font_path` and `font_sha256` metadata so the user can verify the exact font file used.

## Channel Objective

For Pizza Hip'S franchise/F&B business pages, the channel objective is:

- Increase Pizza Hip'S brand awareness.
- Build an audience interested in F&B business, compact store models, and franchise opportunities.
- Educate and nurture people who are considering F&B entrepreneurship.
- Route qualified interest toward Pizza Hip'S franchise/business-model consultation.
- Generate leads and support conversion.

Do not treat franchise pages as generic food/menu pages. Food/product content can be used, but it should support trust, model understanding, demand proof, or lead generation when the page objective is franchise/business.

## Target Audience

Primary audience:

- People aged roughly 22-55 across Vietnam who are interested in F&B business.
- People with capital but too many options and no clear model to choose.
- People who want to start but lack experience, direction, setup knowledge, and operating guidance.
- People who already have premises but do not know what business model to run.
- Small cafes or small local business models that may want to convert or add a new model.

Audience psychology:

- They want to start an F&B business but do not know where to begin.
- They are looking for a suitable business model or product.
- They need practical orientation, operational clarity, and confidence that the model is not vague or overly complex.

## Franchise Page Content Pillars

Use these content routes when the brief is for the Pizza Hip'S franchise/F&B business page:

- General market news that relates to or affects F&B: economy, consumer behavior, rental costs, ingredient costs, food safety, spending trends, and local business shifts.
- F&B industry news: food safety, closures, store model changes, hot food trends, consumer demand, restaurant models, franchise models, operating challenges, and business opportunities.
- Trend-based posts: adapt timely social or market trends into useful F&B business angles.
- Model-focused posts: Pizza Hip'S store model, Hip'S Resto, compact/mobile/kiosk model, product standardization, operating efficiency, and practical setup/operation points.
- Product proof posts: pizza, fried chicken, spicy noodles, combos, menu logic, product appeal, and how product quality supports the business model.

When using market or legal/current-news claims, include source notes for the user to verify unless the final copy is purely brand/opinion content.

## Franchise Page Voice

Use a voice that is:

- Easy to read and practical.
- Not overly academic.
- Clear, direct, and focused on the question raised by the title.
- Natural like a real Facebook page admin, not like a formal business essay or generic AI marketing copy.
- Connected to current events, market movements, or recognizable F&B situations when relevant.
- Connected to local area, weather, season, or daily context when it helps the post feel timely and grounded.
- Concise; avoid rambling or broad essays.

Avoid:

- Dense textbook explanations.
- Vague entrepreneurship motivation with no concrete F&B relevance.
- Unsupported certainty about profit, payback, safety, or guaranteed success.

## Franchise Post Format

For franchise/business posts:

- Title: uppercase, short, strong, pain-point driven, and relevant to what the audience cares about.
- Title should usually start with one relevant icon. It may use punctuation such as quotation marks, exclamation marks, or question marks when natural.
- Opening: 1-2 readable, creative sentences that pull the reader into the issue.
- Body: answer the exact question or promise from the title. If the title asks why compact F&B models are a trend, the body must give direct reasons and analysis.
- Use punctuation and icons only when they improve emphasis and scanability.
- Use icon bullets for key points when the post needs fast mobile scanning.
- Keep the full publish copy under 400 words unless the user explicitly asks for a long-form post.
- Use short paragraphs and short sentence blocks.
- Keep the first 2-3 lines sharp enough to work above the Facebook "see more" fold.
- Do not show labels like "Problem", "Solution", "CTA", "AIDA", or "PAS" in publish-ready copy.
- Avoid over-formatting: no excessive icons, no decorative separators, no repeated all-caps sections after the title, and no filler slogans.
- Include relevant local area/region context when the post targets a place, market, store area, or event.
- Include weather/season/local timing context when it strengthens the hook or product angle and the data is provided or verified. Do not invent current weather.
- Closing: summarize the main idea in 1-2 sentences.
- CTA: make the lead action specific.
- End with one provocative or open-ended question that encourages comment, inbox, or self-reflection when it fits the objective.

Recommended CTA patterns:

- "Chấm để được tư vấn chi tiết."
- "Comment \"ib\" để được gửi báo giá."
- "Muốn tìm giải pháp kinh doanh F&B tinh gọn, liên hệ hotline để được tư vấn chi tiết về mô hình."

If hotline or contact details are missing, use a placeholder or ask for them instead of inventing.

## Copywriting Formulas

Choose the formula based on the post objective:

- AIDA for selling, ads, lead posts, and offer-led content: Attention, Interest, Desire, Action.
- PAS for advisory/lead-magnet posts: Problem, Agitate, Solution.
- BAB for transformation posts: Before, After, Bridge.
- 4C for educational or credibility-driven posts: Clear, Concise, Compelling, Credible.
- Mini-storytelling for emotional or brand-voice posts: a short 30-120 word situation with conflict, resolution, and CTA.

Use the formula as structure, not as visible labels in the publish copy unless the user asks for a breakdown.

## Priority Keywords

Use these keywords naturally when relevant:

- Nhượng quyền tinh gọn.
- Nhượng quyền tối ưu.
- Xe/quầy bán hàng lưu động.
- Tối ưu vận hành.
- Sản phẩm chuẩn hóa.
- Chất lượng đồng nhất.
- Nhượng quyền vốn ít.

## Content Direction

Write for Facebook audiences interested in:

- Pizza, fried chicken, spicy noodles, fast food, casual dining, and food delivery.
- F&B business and franchise opportunities when the page/content route is about franchise.
- Local store awareness, offers, new products, and customer service.

Prioritize:

- Clear customer benefit.
- Appetite appeal.
- Practical reasons to order, visit, or inquire.
- Franchise potential only when the brief is about business or investment.

Avoid:

- Generic food copy with no Pizza Hip'S identity.
- Unsupported business promises.
- Wrong F&B/franchise terms.
- Overclaiming health, profit, or guaranteed results.

## Required Post Elements

Every Pizza Hip'S Facebook post should include:

- Short hook/title.
- Clear body with short paragraphs.
- Local area/region context when relevant to the brief.
- Weather, season, or timing context when relevant and verified/provided.
- Icon scan points when the body uses a list.
- CTA.
- One final provocative or open-ended question when suitable.
- Fixed footer/footage when project details are provided.
- 3-5 accurate hashtags, separated from footer by one blank line.

Do not invent footer details. If address, hotline, or website is missing, ask for it or leave a clear placeholder.

## Hashtags

Use at most 5 hashtags by default unless the user asks for a larger hashtag block.

Choose the 3-5 most accurate hashtags for the post. Do not add weak generic hashtags just to increase count.

The first hashtag should be:

`#pizzahips`

Fixed Pizza Hip'S hashtag pool:

`#pizzahips #hipsresto #nhuongquyen #pizzatime #pizzakieuY #garan #mycay #fastfood #foodanddrink #pizzahealthy #franchise #hipskiosk`

Market/franchise hashtag pool:

`#kinhdoanh #kinhdoanhfnb #fnbvietnam #congdongfnbvietnam #kinhdoanhfnb`

Selection guidance:

- Food/product post: choose up to 5 from `#pizzahips`, `#hipsresto`, `#pizzatime`, `#garan`, `#mycay`, `#fastfood`, `#foodanddrink`.
- Franchise/business post: choose up to 5 from `#pizzahips`, `#nhuongquyen`, `#franchise`, `#hipskiosk`, `#kinhdoanhfnb`, `#fnbvietnam`, `#kinhdoanh`.
- Do not use all hashtags unless the user explicitly asks for a full hashtag bank.

## Image Rules For Pizza Hip'S

For Pizza Hip'S image prompts, briefs, or QA:

- Use orange, blue, and black as the brand color system.
- For text rendered inside the image, use SVN - Bango direction for headline/title text and SVN - Avo direction for supporting/body text when typography is requested.
- Watermark is handled by the existing system/tooling. Do not instruct the agent to add, move, resize, or modify watermark assets.
- Reserve safe zones for existing watermark overlays:
  - Top center is reserved for the Pizza Hip'S logo/brand watermark; keep it visually clean.
  - Bottom right is reserved for hotline/contact/CTA watermark; keep it visually clean.
- Do not place important content in those reserved zones: headline text, prices, CTA, product hero details, faces, QR codes, legal notes, or small readable text.
- When creating image prompts, explicitly leave clean negative space at top center and bottom right so the existing watermark can overlay without covering content.
- On-image text must never be clipped by the canvas edge. Keep at least 5% padding and rerender if any letter is cut off.
- On-image text must not touch or overlap watermark overlays. Treat the top-center logo zone and bottom-right hotline zone as hard no-text areas.
- Create and return one final image by default. Do not generate multiple visual variants unless the Lead explicitly asks for comparison variants.
- If using `create_image` only to generate a textless background before `render_creative`, call `create_image` with `deliver=false` and attach only the final flattened image.
- Minimum 1 image, maximum 10 images for a Facebook post set.
- Prefer realistic product imagery: pizza, fried chicken, spicy noodles, combo meals, store/customer scenes, or kiosk/franchise visuals depending on the brief.
- Do not include logos from other food brands.
- Keep text readable and avoid cutting off product, logo, price, CTA, or hotline.
- For product posts, 1080x1080 px is the preferred square format.

## Suggested Content Patterns

### Product / Menu Post

Use this structure:

1. Hook about taste, craving, combo, occasion, or offer.
2. Short benefit-focused description.
3. CTA to order, inbox, or visit.
4. Footer if available.
5. Hashtags.

### Promotion Post

Use this structure:

1. Offer-led hook.
2. What customers receive.
3. Time/location/condition if provided.
4. CTA.
5. Footer.
6. Hashtags.

### Franchise Post

For franchise posts, use this skill only for brand identity, hashtags, watermark, footer, and visual tone. Use `pizza-hips-franchise-knowledge` for business-model content, legal cautions, and franchise-specific argument structure.

## Final Self-Check

Before returning Pizza Hip'S content, verify:

- Brand name is written consistently as Pizza Hip'S.
- CTA exists.
- Hashtag block starts with `#pizzahips`.
- Hashtags fit the content route: food/product or franchise/business.
- Footer is not invented.
- Image brief uses orange/blue/black and reserves empty/safe space at top center and bottom right for existing watermark overlays.
- No claim is exaggerated beyond the user's brief.
