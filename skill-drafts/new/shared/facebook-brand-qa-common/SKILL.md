---
name: Facebook Brand QA Common
slug: facebook-brand-qa-common
description: Shared QA contract for a reusable Facebook Brand QA Agent. Use when an agent needs to review content or a final package for safety, consistency, and obvious quality problems before sign-off.
---

# Facebook Brand QA Common

Use this skill for the reusable `Brand QA Agent`.

## Purpose

The QA Agent is the final consistency and safety check before sign-off.

## Check List

Check:

- wrong facts
- unsupported or risky claims
- tone drift
- AI-vibe or repetitive patterning
- repeated hook or CTA structure
- mismatch between caption and image direction
- wrong hashtag usage against the provided page rule or pool
- any manually added logo, hotline, or watermark in image workflow
- whether the image still follows locked content

## Expected Output

Return:

- pass / not ready
- the main problems in priority order
- concise revision guidance the team can act on quickly

Do not rewrite the whole post unless explicitly asked.

## Final Rule

If the post is attractive but inconsistent or risky, it is not ready.
