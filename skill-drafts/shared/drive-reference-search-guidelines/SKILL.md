---
name: Drive Reference Search Guidelines
slug: drive-reference-search-guidelines
description: Workflow for design agents to find the right image references inside allowed Drive or cache folders before creating new images. Use when a Designer must search brand assets, product photos, project photos, campaign folders, or Drive cache; inspect a limited number of images; select 1-3 best references; download selected images; and pass local MEDIA paths into the approved image-generation workflow without scanning all Drive or inventing assets.
---

# Drive Reference Search Guidelines

Use this skill when a design agent needs to search Google Drive, Drive cache, brand folders, project folders, product folders, or shared media folders to find real reference images before creating a new image.

This skill is brand-neutral. Combine it with the page or brand skill, visual skill, and brief-specific constraints.

## Core Rule

Use real available references, but search narrowly.

Do not scan the whole Drive. Do not inspect unlimited images. Do not use files outside granted folders. If no relevant reference exists, report the blocker clearly instead of inventing that a product or project image exists.

## Hard Limits

- Only search folders explicitly available or granted by the current tool or session.
- Do not scan all Drive.
- Do not list more than needed from each folder.
- List at most 30 image candidates before selecting images to inspect.
- Inspect at most 20 images total.
- Select 1-3 best references by default.
- Download only selected references, not every candidate.
- Prefer product- or project-specific references over generic visuals.
- If references are too weak, say so and ask for better assets or permission to use a broader folder.

## Required Inputs

Expect from the Lead or user:

- Page or brand.
- Product, service, project, or topic.
- Asset type and size or aspect ratio.
- Campaign or content message.
- Required visual subject.
- Allowed Drive or cache folders, folder IDs, or approved search scope.
- Any folder naming hints, project names, product names, dates, file naming patterns, or aliases.
- Whether final output needs references for image creation or only an image brief.

If allowed folders are missing, ask for them or state that Drive reference search is blocked.

## Search Workflow

### 1. Parse The Brief

Extract:

- brand or page name
- product or project name
- asset goal
- main visual subject
- required mood or style
- must include and must avoid
- safe zones, watermark zones, and logo zones
- whether references should be product, project, site, people, package, building, interior, food, construction, or technical diagram

### 2. Generate Search Keywords

Create keyword groups before searching:

```text
PRIMARY:
- exact product/project/brand terms

LOCAL LANGUAGE:
- local-language aliases if relevant

ENGLISH:
- English industry terms

ALIASES:
- abbreviations, spelling variants, old product names, code names, SKU-like terms

VISUAL:
- package, site, model, render, before-after, detail, close-up, finished product, ingredient, facade, interior, slab, kiosk
```

Examples:

- Baking: `bot mi`, `Dia Cau 999`, `flour`, `wheat flour`, `bao bi`, `thanh pham`, `banh trung thu`
- Construction: `san Ubot`, `UBoot Beton`, `flat slab`, `voided slab`, `cong trinh`, `thi cong`, `mat cat`, `BIM`
- Franchise or F&B: `Pizza Hip'S`, `kiosk`, `combo`, `pizza`, `ga ran`, `store`, `menu`, `product`

### 3. List Allowed Folders

List only the allowed or granted root folders.

Evaluate folder names by:

- brand or page match
- product, project, or topic match
- date or campaign relevance
- folder type such as `brand assets`, `product photos`, `project photos`, `campaign`, `creative`, `before-after`, `render`, `logo`, `raw`, `final`

Do not open unrelated folders just because they are nearby.

### 4. Choose Candidate Folders

Choose the most likely folders:

- Product brief: prioritize product, photo, or package folders.
- Project or construction brief: prioritize project folders, site photos, drawings or renders, and before-after folders.
- Brand or identity brief: prioritize brand kit, logo, guideline, and approved creative folders.
- Recipe or food brief: prioritize ingredient, product, and finished dish photos.
- Franchise or store brief: prioritize store, kiosk, menu, product, and customer-scene folders.

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

Use file names, folder paths, modified dates, and thumbnails or metadata if available to rank candidates before inspecting images.

### 6. Inspect Image Candidates

Inspect at most 20 images total.

Inspect in priority order:

1. exact product or project match
2. clear subject match
3. recent or campaign-specific asset
4. approved or final asset over raw asset
5. higher quality and less obstructed image

Stop early when you already have 1-3 strong references.

### 7. Score References

Score each inspected image:

```text
RELEVANCE:
- Does it show the exact product/project/subject?

AUTHENTICITY:
- Is it a real brand/product/project image?

QUALITY:
- Is it sharp, well-lit, high enough resolution, and not heavily cropped?

USABILITY:
- Can it guide the new image without covering key details?

RIGHTS/SCOPE:
- Is it inside allowed folders and safe to use for this task?

BRAND FIT:
- Does it match page visual tone and current campaign?
```

Prefer references that are:

- real product photos for product posts
- real project or site photos for construction posts
- real store, kiosk, menu, or product photos for F&B posts
- real ingredient or finished cake photos for baking posts
- approved brand assets for identity-heavy posts

### 8. Select 1-3 References

Choose:

- 1 primary reference for exact subject
- 1 secondary reference for style or context if useful
- 1 technical or detail reference if the asset needs accuracy

Do not choose more than 3 references unless the user explicitly asks and tooling supports it.

### 9. Download Selected References

Download only the selected references using the approved Drive or cache workflow available in the current environment.

Keep:

- original Drive file name or ID
- local downloaded path
- reason selected
- any limitation such as low resolution or partial view

Use the downloaded local MEDIA path as the reference input for the approved image-generation workflow.

If the image-generation tool supports multiple references, pass the 1-3 selected local paths in priority order.

### 10. Create Or Brief The Image

When generating a final image:

- Use the selected reference path(s).
- Preserve brand, product, or project identity.
- Add brand-specific watermark safe-zone rules.
- Avoid asking the image model to invent exact logos or exact text if deterministic rendering is required.
- If exact on-image text is required, use the brand font or render workflow where available.

When returning only a brief:

- Include selected reference paths and why each was chosen.
- Mention limitations.

## Reference Use Discipline

When references are available, use them to understand:

- product shape
- environment
- material
- page visual taste
- composition pattern
- realism level

Do not copy a reference blindly.
Do not invent missing brand assets from nearby reference cues.
If exact product or location accuracy matters, ask for the real asset or use a placeholder.

## Blocker Rules

Report a blocker instead of guessing when:

- no allowed folder is provided
- search tools cannot access granted folders
- no relevant product or project image is found
- all candidate images are too low quality or unrelated
- the asset needed is outside granted folders
- the task requires a real product or project reference but only generic images are available

Blocker format:

```text
BLOCKER:
I could not find a usable reference image for [subject] inside the allowed folders.

SEARCHED:
- [folder/path]
- [keywords]
- [candidate count/inspect count]

NEEDED:
- [specific missing asset or permission]
```

## Privacy And Scope Rules

- Do not expose private Drive paths unless needed for the task handoff.
- Do not use confidential project or client images outside the requested context.
- Do not mention internal folder names in public-facing copy.
- Do not use people or faces from project folders if the brief does not need people or permission is unclear.
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
- Pass the selected reference path(s) into the approved image-generation workflow.
```

For a final generated image task, do not over-explain. Return the final image and concise notes about which reference image(s) guided it.

## Self-Check

Before finishing, verify:

- search stayed inside allowed folders
- no whole-Drive scan occurred
- no more than 30 image candidates were listed
- no more than 20 images were inspected
- selected references are real, relevant, and downloaded
- local MEDIA paths are available for image generation
- missing assets are reported as blockers, not invented
