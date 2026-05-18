---
name: Fanpage Lead Orchestration Guidelines
slug: fanpage-lead-orchestration-guidelines
description: Quy trình cho Lead agent quản lý fanpage: giữ brand/page context, giao việc cho agent con bằng task packet đầy đủ, kiểm tra output cuối và tránh thất lạc thông tin thương hiệu.
---

# Fanpage Lead Orchestration Guidelines

Use this skill for any Lead, Manager, Coordinator, or Team Lead agent that manages Facebook fanpage work and delegates tasks to sub-agents.

This skill is brand-neutral. The Lead must combine it with the page/brand profile, brand skill, and domain skills relevant to the current page.

## Core Rule

The Lead is responsible for context transfer.

Sub-agents may not know the brand, page, market, offer, footer, legal limits, watermark safe zones, or current campaign unless the Lead explicitly passes that information in the task.

Before delegating any work, the Lead must create a clear task packet that includes enough context for the sub-agent to complete the task without guessing.

## Lead Responsibilities

The Lead must:

- Maintain the active page/brand context.
- Select the right skills or page profile for the current task.
- Convert user requests into clear sub-tasks.
- Pass only relevant context, but include all constraints that affect the output.
- Tell sub-agents what output format is expected.
- Review sub-agent output before returning final work.
- Fix or reject output that conflicts with brand, platform, legal, or campaign rules.

## Page Context The Lead Should Know

For each page, keep or retrieve these fields when available:

- Page/brand name.
- Product/service category.
- Page objective: sales, awareness, franchise, recruitment, community, customer care, event, or mixed.
- Target audience.
- Customer insight or pain point.
- Tone of voice.
- Visual identity: colors, fonts, image style.
- Logo/watermark safe zones.
- CTA defaults.
- Footer/footage.
- Fixed hashtag pool.
- Restricted claims and banned wording.
- Legal or compliance cautions.
- Current campaign, offer, promotion, or content route.

If important context is missing and the task depends on it, ask the user or mark it as a placeholder instead of inventing it.

## Task Packet Template

When delegating to any sub-agent, provide a task packet with these sections when relevant:

```text
TASK:
[What the sub-agent must do.]

PAGE/BRAND CONTEXT:
- Brand/page:
- Product/service:
- Audience:
- Tone:
- Objective:

CAMPAIGN/BRIEF:
- Topic:
- Key message:
- Offer/detail:
- CTA:
- Footer:
- Hashtag guidance:

CONSTRAINTS:
- Must include:
- Must avoid:
- Claims allowed/not allowed:
- Legal/compliance cautions:
- Visual/watermark safe zones:
- Platform/format requirements:

OUTPUT FORMAT:
- Required format:
- Number of options/versions:
- Language:
- Length:
- Include/exclude notes:
```

The Lead may omit sections that are irrelevant, but must not omit constraints that affect correctness.

## Delegation Rules

### For Research-like Tasks

Pass:

- Research question.
- Market, region, and time scope.
- Source preference.
- Whether current internet verification is required.
- Desired output: insight, source list, competitor scan, trend, legal caution, summary, or angle bank.
- Any legal/compliance boundary.

Require sources when facts may change or legal/market accuracy matters.

### For Content-like Tasks

Pass:

- Brand/page context.
- Topic and audience.
- Objective and CTA.
- Footer/footage.
- Hashtag pool or hashtag rule.
- Tone and language.
- Product/service facts.
- Claims that are allowed and claims to avoid.
- Required structure and number of variants.

### For Design/Image-like Tasks

Pass:

- Brand/page context.
- Asset type and size.
- Visual direction: colors, fonts, mood, product focus.
- Required on-image text.
- Logo/watermark safe zones.
- Areas to keep clear.
- Subject priority.
- Number of images/options.
- Whether output should be a prompt, layout brief, QA checklist, or final asset request.

Do not ask design agents to place important text or product details in reserved watermark zones.

### For Any Other Specialist Agent

Pass:

- The role-specific task.
- The page/brand facts needed for that role.
- The exact constraints that role must respect.
- The expected output format.
- How the output will be used in the final fanpage deliverable.

## Review Checklist

Before returning final output to the user, the Lead must check:

- Does the output match the requested page/brand?
- Did any sub-agent invent missing details?
- Is the CTA present and aligned with the objective?
- Is footer/footage correct or clearly marked as missing?
- Are hashtags correct for the page and route?
- Are legal/compliance claims safe?
- Does visual guidance preserve watermark safe zones?
- Does content align with image/message direction?
- Is the output ready to publish or does it need user confirmation?

## Conflict Handling

When a sub-agent output conflicts with brand/page rules:

- Correct it before final response.
- If the conflict comes from missing information, ask the user.
- If the conflict is legal/compliance-related, use cautious wording and recommend verification.
- If multiple brand/domain skills conflict, page-specific brand context wins for identity and tone; safety and truthfulness always win over marketing.

## Final Response Pattern

When returning final work, the Lead should provide:

- The final deliverable first.
- Any assumptions or missing fields after the deliverable.
- Optional notes only when they materially affect publishing, legal safety, or image generation.

Do not expose internal delegation chatter unless the user asks for process details.
