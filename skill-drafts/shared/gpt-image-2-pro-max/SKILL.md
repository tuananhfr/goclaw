---
name: gpt-image-2-pro-max
description: "Prompt support layer for GPT-Image-2 / OpenAI image generation. Provides a searchable corpus of 7,405 community-vetted prompts and 9,903 reference images so image-making agents can find strong precedent, adapt prompt structure, and improve final image generation quality. Use when an image agent needs prompt support, reference logic, or prompt research for a real GPT-Image-2 task. Do not treat this skill as the default final output layer when the true goal is to generate the image asset."
category: ai
keywords: [gpt-image-2, image-prompts, prompt-library, openai-image, ai-image, prompt-engineering, ad-creative, product-photography]
metadata:
  author: Richard Ng
  version: "1.0.0"
  endpoint: "https://gpt-image-2-prompts.goclawoffice.com"
---

# GPT-Image-2 Prompt Support

This skill is a prompt-support layer for image-making agents.

It helps an agent:

- search strong precedent prompts
- inspect tagged reference examples
- extract reusable prompt structure
- adapt prompt logic to the current brief
- improve GPT-Image-2 generation quality before image creation

This skill should usually be used together with a visual or image-making skill, not as a replacement for actual image creation.

## Core Principle

Prompt support is not the final job.

If the real task is to create an image, use this skill to improve the generation prompt, then continue to the actual image-generation step.

## What This Skill Contains

1. `scripts/search.py`
Thin HTTP client over a hosted corpus of 7,405 community-vetted prompts and 9,903 reference images. Results are BM25-ranked and tagged across 10 facets.

2. Hosted prompt corpus
Searchable prompt records with prompt body, attribution, tags, and reference image.

The tool finds candidates. The agent owns the judgement:

- which base to use
- which parts to keep literal
- which parts to adapt
- which mood, lighting, and composition logic truly fit the brief

## When To Apply

### Must use

- An image-making agent needs prompt support for a real GPT-Image-2 generation task.
- The user explicitly wants a GPT-Image-2 or OpenAI image-generation prompt for a real production task.
- The user wants attributed examples of strong prompt structure.
- The agent needs better visual precedent before generating an image.

### Skip

- The user wants the image rendered and no prompt support is needed.
- The task is unrelated to image prompts.
- The user already has a finished prompt and only wants it executed.

For fanpage visual work, do not stop at this skill if the true requested output is a final image.

## Recommended Workflow

For any production prompt-support request:

```text
1. Diagnose the brief.
2. Search the hosted prompt corpus.
3. Pick the strongest mood-aligned base.
4. Refactor the base into generation-ready prompt logic.
5. Resolve the prompt against the current brief.
6. If the real task is image creation, continue to actual image generation.
```

## Hosted Backend

```text
Endpoint: https://gpt-image-2-prompts.goclawoffice.com
```

The hosted backend contains 7,405 community-vetted prompts and 9,903 reference images indexed across these facets:

- subjects
- styles
- lighting
- cameras
- moods
- palettes
- compositions
- mediums
- techniques
- usecases

Each record carries the prompt body, attribution, and a reference image.
Rate-limited per IP. Use fairly and do not scrape.

## CLI

```text
search.py [query] [--shape SHAPE] [--has-image] [-n N] [--full] [--persist PATH]
```

Example usage:

```bash
python scripts/search.py "luxury shoe ecommerce ad cream pastel" -n 5
python scripts/search.py "perfume bottle" --shape ecommerce -n 3
python scripts/search.py "neon ui" --persist plans/neon-refs.md
```

Filter knobs:

- `--shape` - portrait | poster | ui | character | comparison | ecommerce | ad | thumbnail | infographic | comic
- `--has-image` - only records with a reference image
- `-n N` - top N, default 5
- `--full` - do not truncate prompt body
- `--persist PATH` - write top results to a markdown file with embedded reference images

## Output Anatomy

```text
#1  bm25=-15.59  shape=ecommerce
  id    : z9q36mnc
  title : Futuristic Bionic Super Shoe
  author: @<creator>
  tweet : https://x.com/<creator>/status/<tweet_id>
  image : <reference image URL>
  tags  : subjects=product,fashion-item | styles=cinematic | cameras=low-angle |
          moods=luxurious,intense,futuristic | palettes=gold-black |
          techniques=parameterised-template
  prompt:
    Extreme futuristic {argument name="subject" default="cheetah bionic super shoe"} ...
```

## Prompt Support Rules

- Search for prompt structure, not blind copying.
- Prefer mood and composition fit over surface similarity.
- Keep what is strong from the base prompt.
- Rewrite what is brief-specific.
- Do not carry over irrelevant product details from the base prompt.
- Do not invent brand assets, claims, or exact text.
- If the final job is image creation, pass the improved prompt into the image-generation workflow instead of returning prompt text as the final deliverable by default.

## Final Rule

This skill improves prompt quality for GPT-Image-2.
If the actual task is to produce an image, do not stop here.
Use the output of this skill to help generate a stronger final asset.
