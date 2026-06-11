---
name: Facebook Content Workflow
slug: facebook-content-workflow
description: Shared workflow state rules for Facebook page teams. Use when a lead page agent coordinates research, writing, approval, image creation, and publishing across specialist agents. This skill keeps the process strict where it matters without forcing one creative style.
---

# Facebook Content Workflow

Use this skill for the lead page agent that coordinates a Facebook content team.

This skill controls workflow state.
It does not decide page-specific tone, products, or claims.

## Core Rule

The team should be free in thinking, but strict in sequence.

Keep these workflow locks:

- content first
- image after content
- no publishing without explicit approval
- no fake branding added by image agents

## Required Order

Default order:

1. read the structured source if provided
2. research if needed
3. develop content
4. review and lock content
5. create image from locked content
6. apply watermark through system workflow
7. publish only if the user explicitly says `POST_NOW`

## Structured Source Rule

If docs, checklist, spreadsheet, or brief files exist:

- read them before assigning work
- treat them as the main source of truth
- do not skip them because chat gave a short version

## Content Lock Rule

Content must be treated as locked once approved.

After content is locked:

- image agents must follow the locked content
- do not rewrite the caption inside image workflow
- do not silently change the core angle, claim, CTA, or route

## Image Timing Rule

Do not create final images before content is approved.

Exception:

- only when the user explicitly asks for concept exploration before content approval

Even in that case:

- keep outputs as concept exploration, not final post assets

## Branding Asset Rule

Image agents must not:

- draw logo manually
- add watermark manually
- invent hotline
- invent address
- invent footer-like text

Branding overlays must come from the approved system workflow when available.

## Publishing Rule

Do not publish unless the user explicitly says:

`POST_NOW`

Without that command:

- prepare preview only

## Blocker Rule

Block only after checking the relevant source and required workflow step.

Blockers should be:

- short
- specific
- about the main blocking issue only

## Final Rule

This skill protects process state, not creativity.
Let specialists think freely inside the workflow locks above.
