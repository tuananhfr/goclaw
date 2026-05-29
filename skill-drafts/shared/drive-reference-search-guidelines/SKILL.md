---
name: Drive Reference Search Guidelines
description: Workflow for design agents to find the right image references inside allowed Drive/cache folders before creating new images. Use when a Designer must search brand assets, product photos, project photos, campaign folders, or Drive cache; inspect a limited number of images; select 1-3 best references; download selected images; and pass local MEDIA paths to image generation without scanning all Drive or inventing assets.
---

# Drive Reference Search Guidelines

Use this skill when a design agent needs to search Google Drive, Drive cache, brand folders, project folders, product folders, or shared media folders to find real reference images before creating a new image.

This skill is brand-neutral. Combine it with the page/brand skill, design skill, and brief-specific constraints.

## Core Rule

Use real available references, but search narrowly.

Do not scan the whole Drive. Do not read unlimited images. Do not use files outside granted folders. If no relevant reference exists, report the blocker clearly instead of inventing that a product/project image exists.

## Hard Limits

- Only search folders explicitly available or granted by the current tool/session.
- Do not read all Drive.
- Do not list more than needed from each folder.
- List at most 30 image candidates before selecting images to inspect.
- Use `read_image` on at most 20 images total.
- Select 1-3 best references by default.
- Download only selected references, not every candidate.
- Prefer product/project-specific references over generic visuals.
- If references are too weak, say so and ask for better assets or permission to use a broader folder.

## Required Inputs

Expect from the Lead or user:

- Page/brand.
- Product, service, project, or topic.
- Asset type and size/aspect ratio.
- Campaign/content message.
- Required visual subject.
- Allowed Drive/cache folders or folder IDs.
- Any folder naming hints, project names, product names, dates, file naming patterns, or aliases.
- Whether final output needs reference images or only an image brief.

If allowed folders are missing, ask for them or state that Drive reference search is blocked.

## Search Workflow

### 1. Parse The Brief

Extract:

- Brand/page name.
- Product or project name.
- Asset goal.
- Main visual subject.
- Required mood/style.
- Must include / must avoid.
- Safe zones, watermark zones, and logo zones.
- Whether references should be product, project, site, people, package, building, interior, food, construction, or technical diagram.

### 2. Generate Search Keywords

Create keyword groups before searching:

```text
PRIMARY:
- exact product/project/brand terms

VIETNAMESE:
- Vietnamese topic/product aliases

ENGLISH:
- English industry terms

ALIASES:
- common abbreviations, spelling variants, old product names, code names, SKU-like terms

VISUAL:
- package, site, model, render, before-after, detail, close-up, finished product, ingredient, facade, interior, slab, kiosk, etc.
```

Examples:

- Baking: `bột mì`, `Địa Cầu 999`, `flour`, `wheat flour`, `bao bì`, `thành phẩm`, `bánh Trung thu`.
- Construction: `sàn Ubot`, `UBoot Beton`, `flat slab`, `voided slab`, `công trình`, `thi công`, `mặt cắt`, `BIM`.
- Franchise/F&B: `Pizza Hip'S`, `kiosk`, `combo`, `pizza`, `gà rán`, `store`, `menu`, `product`.

### 3. List Allowed Folders

List only the allowed/granted root folders.

Evaluate folder names by:

- Brand/page match.
- Product/project/topic match.
- Date/campaign relevance.
- Folder type: `brand assets`, `product photos`, `project photos`, `campaign`, `creative`, `before-after`, `render`, `logo`, `raw`, `final`.

Do not open unrelated folders just because they are nearby.

### 4. Choose Candidate Folders

Choose the most likely folders:

- Product brief: prioritize product/photo/package folders.
- Project/construction brief: prioritize project folder, site photos, drawings/renders, before-after folders.
- Brand/identity brief: prioritize brand kit, logo, guideline, approved creative folders.
- Recipe/food brief: prioritize ingredient/product photos and finished dish photos.
- Franchise/store brief: prioritize store, kiosk, menu, product, customer scene folders.

If folders are organized by project, open only 1-3 project folders that best match the brief.

### 5. List Image Candidates

List at most 30 image files total.

Prefer likely image formats:

- `.jpg`
- `.jpeg`
- `.png`
- `.webp`
- `.heic`
- `.tif`
- `.tiff`

Use file names, folder paths, modified dates, and thumbnails/metadata if available to rank candidates before reading images.

### 6. Read Image Candidates

Use `read_image` on at most 20 images total.

Read in priority order:

1. Exact product/project match.
2. Clear subject match.
3. Recent or campaign-specific asset.
4. Approved/final asset over raw asset.
5. Higher quality and less obstructed image.

Stop early when you already have 1-3 strong references.

### 7. Score References

Score each inspected image:

```text
RELEVANCE:
- Does it show the exact product/project/subject?

AUTHENTICITY:
- Is it a real brand/product/project image?

QUALITY:
- Is it sharp, well-lit, high enough resolution, not heavily cropped?

USABILITY:
- Can it guide the new image without covering key details?

RIGHTS/SCOPE:
- Is it inside allowed folders and safe to use for this task?

BRAND FIT:
- Does it match page visual tone and current campaign?
```

Prefer references that are:

- Real product photos for product posts.
- Real project/site photos for construction posts.
- Real store/kiosk/menu/product photos for F&B posts.
- Real ingredient/finished cake photos for baking posts.
- Approved brand assets for identity-heavy posts.

### 8. Select 1-3 References

Choose:

- 1 primary reference for exact subject.
- 1 secondary reference for style/context if useful.
- 1 technical/detail reference if the asset needs accuracy.

Do not choose more than 3 references unless the user explicitly asks and tooling supports it.

### 9. Download Selected References

Use `gdrive_download` only for selected references.

Keep:

- original Drive file name or ID
- local downloaded path
- reason selected
- any limitation such as low resolution or partial view

Use the downloaded `MEDIA` path as the reference input for image generation, such as:

```text
create_image.reference_image_path = [local MEDIA path]
```

If the image generation tool supports multiple references, pass the 1-3 selected local paths in priority order.

### 10. Create Or Brief The Image

When generating a final image:

- Use the selected reference path(s).
- Preserve brand/product/project identity.
- Add brand-specific watermark safe zone rules.
- Avoid asking the image model to invent exact logos/text if deterministic rendering is required.
- If exact on-image text is required, use the brand font/render workflow where available.

When returning only a brief:

- Include selected reference paths and why each was chosen.
- Mention limitations.

## Blocker Rules

Report a blocker instead of guessing when:

- No allowed folder is provided.
- Search tools cannot access granted folders.
- No relevant product/project image is found.
- All candidate images are too low quality or unrelated.
- The asset needed is outside granted folders.
- The task requires a real product/project reference but only generic images are available.

Blocker format:

```text
BLOCKER:
I could not find a usable reference image for [subject] inside the allowed folders.

SEARCHED:
- [folder/path]
- [keywords]
- [candidate count/read count]

NEEDED:
- [specific missing asset or permission]
```

## Privacy And Scope Rules

- Do not expose private Drive paths unless needed for the task handoff.
- Do not use confidential project/client images outside the requested context.
- Do not mention internal folder names in public-facing copy.
- Do not use people/faces from project folders if the brief does not need people or permission is unclear.
- Do not use unrelated brand logos.

## Final Response Pattern

For a reference search result, return:

```text
SELECTED REFERENCES
- [local MEDIA path] - [why selected]
- ...

SEARCH SUMMARY
- Allowed folders searched:
- Candidates listed:
- Images inspected:
- Blockers/limits:

NEXT USE
- Pass primary reference to create_image.reference_image_path.
```

For a final generated image task, do not over-explain. Return the final image and concise notes about which reference image(s) guided it.

## Self-Check

Before finishing, verify:

- Search stayed inside allowed folders.
- No whole-Drive scan occurred.
- No more than 30 image candidates were listed.
- No more than 20 images were read.
- Selected references are real, relevant, and downloaded.
- Local MEDIA paths are available for image generation.
- Missing assets are reported as blockers, not invented.
