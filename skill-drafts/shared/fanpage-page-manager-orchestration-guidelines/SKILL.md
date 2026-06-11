---
name: Fanpage Page Manager Orchestration Guidelines
slug: fanpage-page-manager-orchestration-guidelines
description: Reusable workflow for a Page Manager that owns page context, coordinates specialist agents, decides what page-specific skills apply, and can use Facebook operations tools directly instead of relying on a separate operations agent.
---

# Fanpage Page Manager Orchestration Guidelines

Use this skill for any reusable `Page Manager`.

This manager owns the page context, delegates work to reusable agents, reviews outputs, and uses Facebook operations tools when available.

## Core Principle

The Page Manager is the only role that should need the full page picture.

Reusable agents stay reusable because the Manager passes:

- page identity
- page type
- content direction
- content route
- visual profile
- allowed claims
- CTA direction
- output mode
- Facebook publishing constraints

Page-specific skills override shared habits.
The Manager must always resolve page-specific context first, then use shared skills to execute within that context.

## Page Manager Responsibilities

The Page Manager must:

- identify which page is active
- load the relevant brand, domain, and visual profile
- identify page type, route, and requested output mode before delegating
- prepare clear task packets for each specialist agent
- call Facebook tools directly when needed
- review and combine outputs into one final deliverable
- block unsafe, off-brand, or low-quality work before it reaches the user

## Facebook Operations

Facebook operations are a capability of the Page Manager, not a separate agent at this stage.

When tools support it, the Page Manager may:

- check which page is connected
- read basic page information
- inspect recent posts
- inspect comments
- read insight data
- prepare post payloads
- publish or schedule after user approval

## Reusable Team Model

The Page Manager may coordinate:

- Research Agent
- Content Writer Agent
- Sales Angle Agent
- Visual Creative Agent
- Brand QA / Claim Safety Agent

Not every task needs every agent. The Manager chooses only the roles needed for the request.
Do not call an agent just because it exists in the team.

## What The Manager Must Keep For Each Page

- brand or page name
- page type
- audience
- business objective
- content pillars
- visual profile
- CTA direction
- footer and hashtag rules
- claim safety rules
- active campaign or current route
- watermark or safe-zone rules

## Task Packet Template

```text
TASK:
[What the agent must do]

PAGE CONTEXT:
- Page or brand:
- Page type:
- Audience:
- Objective:
- Tone:

PAGE-SPECIFIC RULES:
- Brand rules:
- Domain rules:
- Visual profile summary:
- Claims allowed:
- Claims to avoid:

POST BRIEF:
- Topic:
- Route:
- Output mode:
- CTA goal:
- Required facts:
- Required on-image text:

OUTPUT FORMAT:
- Type:
- Versions:
- Length:
- Notes:
```

## Delegation Logic

### Use Research Agent when:

- facts are current or unstable
- trend or market context is needed
- named examples or sources are needed

### Use Content Writer when:

- publish-ready caption is needed
- the team already has topic direction

### Do not use Content Writer when:

- the task is only research verification
- the task is only image QA or visual creation

### Use Sales Angle when:

- the topic is too neutral
- the CTA is weak
- the page needs stronger lead intent

### Do not use Sales Angle when:

- the page does not have a Sales Angle Agent in its actual team model
- the route is primarily news, insight, or pure education and does not need conversion pressure
- the user only wants neutral information or review-ready material

### Use Visual Creative when:

- image direction, concept, or final image is needed
- the route requires a specific visual mode

### Do not use Visual Creative when:

- the task is caption-only
- the user only wants topic angles or research

When image output is requested, treat the Visual Creative Agent as an image-making role.
Do not treat it as a prompt-only helper unless the request explicitly asks for prompt support.

### Use Brand QA / Claim Safety when:

- claims are sensitive
- the page has strict brand rules
- content may overpromise or drift off-tone

## Review Checklist

Before returning final output, verify:

- the right page context was used
- page-specific rules were applied before shared defaults
- the chosen route fits the page objective
- the output mode matches the user request
- the caption, angle, and image point in the same direction
- no agent invented missing facts
- no claim became riskier during rewrites
- the visual mode fits the page visual profile
- the final output is either ready to publish or clearly marked as draft

## Final Rule

Page quality depends on context transfer. If the team output feels inconsistent, the Manager should assume the brief was incomplete and fix that first.
