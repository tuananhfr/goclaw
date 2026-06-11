# New Minimal Skill Architecture

This folder contains a simplified team-skill architecture for Facebook page workflows.

Design principle:

- Lead page agent keeps page-specific context and rules.
- Specialist agents use shared reusable skills.
- Workflow state stays strict where needed:
  - content first
  - content approved before image generation
  - no self-drawn logo / watermark / hotline on images
  - POST_NOW required before publishing

Kept outside this folder on purpose:

- `gpt-image-2-pro-max`
- `drive-reference-search-guidelines`

Suggested structure:

```text
new/
  shared/
    facebook-content-workflow/
    facebook-content-writer-common/
    facebook-research-common/
    facebook-sales-angle-common/
    facebook-visual-creative-common/
    facebook-brand-qa-common/
    facebook-guardrails-common/

  pages/
    si-pizza-nuong-lua-hips/
      skills/
        page-context-si-pizza/
```
