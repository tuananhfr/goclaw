/**
 * MCP Tool registrations — Media/Photo domain.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { GraphAPIError } from "../graph-client.js";
import { PageRegistry } from "../page-registry.js";
import type { WatermarkConfig } from "../watermark.js";

const postCommentsSchema = z.object({
  comments: z.array(z.union([
    z.string(),
    z.object({
      message: z.string(),
      rationale: z.string().optional(),
    }),
  ])).optional(),
}).passthrough().optional().describe("Optional final comments for GoClaw to schedule after this post. Facebook MCP ignores this field.");

function errorResult(err: unknown, pageId?: string) {
  const msg = err instanceof GraphAPIError ? err.message : String(err);
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `❌ Error: ${prefix}${msg}` }], isError: true };
}

function ok(data: unknown, label: string, pageId?: string) {
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `✅ ${prefix}${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

function hasWatermark(cfg?: WatermarkConfig): boolean {
  if (!cfg?.enabled) return false;
  if (cfg.items?.length) return cfg.items.some(hasWatermarkItem);
  return hasWatermarkItem(cfg);
}

function hasWatermarkItem(item?: WatermarkConfig): boolean {
  if (!item?.enabled) return false;
  if (item.mode === "text") return Boolean(item.text?.trim());
  return Boolean(item.logo_path || item.logo_url);
}

export function registerMediaTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_get_watermark_config",
    "Get the configured watermark settings for a Facebook Page. Use this before posting images; GoClaw should apply the watermark locally for preview, then post the final image without MCP auto-watermarking.",
    {
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ page_id }) => {
      try {
        const { pageId, watermark } = registry.getWatermarkConfig(page_id);
        return ok(
          {
            page_id: pageId,
            enabled: watermark?.enabled === true,
            has_watermark: hasWatermark(watermark),
            watermark,
          },
          `Watermark config for page ${pageId}`,
          pageId,
        );
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_apply_watermark",
    "Apply the configured Page watermark to an image without posting it to Facebook. Use this before previewing or persisting generated images.",
    {
      image_url: z.string().describe("Public URL, GoClaw /v1/files path, MEDIA: path, or local file path of the image"),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ image_url, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.applyWatermarkToImage(image_url);
        const dataUrl = `data:${result.contentType};base64,${result.data}`;
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({
                page_id: client.getPageId(),
                filename: result.filename,
                content_type: result.contentType,
                size_bytes: result.sizeBytes,
                data_url: dataUrl,
              }),
            },
            {
              type: "image" as const,
              data: result.data,
              mimeType: result.contentType,
            },
          ],
        };
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_upload_photo",
    "Upload a photo to the Page without publishing it. Returns a media ID that can be used with fb_create_post_with_media to create a post with multiple photos.",
    {
      image_url: z.string().describe("Public URL or local file path of the image to upload"),
      caption: z.string().optional().describe("Optional caption for the photo"),
      apply_watermark: z.boolean().default(false).describe("Compatibility option. Default false because GoClaw should apply watermark before preview/posting."),
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ image_url, caption, apply_watermark, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.uploadPhoto(image_url, caption, false, apply_watermark);
        return ok(result, `Photo uploaded (unpublished) — ID: ${result.id}\nUse this ID with fb_create_post_with_media.`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );

  server.tool(
    "fb_create_photo_post",
    "Post a single photo with a caption directly to the Page (published immediately). For multi-photo posts, use fb_upload_photo + fb_create_post_with_media instead.",
    {
      image_url: z.string().describe("Public URL or local file path of the image to post"),
      caption: z.string().describe("Caption/message for the photo post"),
      apply_watermark: z.boolean().default(false).describe("Compatibility option. Default false because GoClaw should apply watermark before preview/posting."),
      post_comments: postCommentsSchema,
      page_id: z.string().optional().describe("Optional page ID. If omitted, uses the default page."),
    },
    async ({ image_url, caption, apply_watermark, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const result = await client.createPhotoPost(image_url, caption, apply_watermark);
        return ok(result, `Photo post published — ID: ${result.id ?? result.post_id}`, client.getPageId());
      } catch (e) { return errorResult(e, page_id); }
    },
  );
}
