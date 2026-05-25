/**
 * MCP Tool registrations - deterministic creative rendering.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { renderCreative } from "../creative-renderer.js";
import { GraphAPIError } from "../graph-client.js";
import { PageRegistry } from "../page-registry.js";

function errorResult(err: unknown, pageId?: string) {
  const msg = err instanceof GraphAPIError ? err.message : err instanceof Error ? err.message : String(err);
  const prefix = pageId ? `[Page: ${pageId}] ` : "";
  return { content: [{ type: "text" as const, text: `Error: ${prefix}${msg}` }], isError: true };
}

const textLayerSchema = z.object({
  text: z.string().min(1),
  font_path: z.string().min(1),
  font_size: z.number().positive(),
  color: z.string().min(1),
  x_pct: z.number().min(0).max(100),
  y_pct: z.number().min(0).max(100),
  max_width_pct: z.number().min(1).max(100).optional(),
  align: z.enum(["left", "center", "right"]).optional(),
  line_height: z.number().positive().optional(),
  letter_spacing: z.number().optional(),
  opacity: z.number().min(0).max(100).optional(),
});

export function registerCreativeTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_render_creative",
    "Render on-image text using exact font files, returning a preview image and font path/hash metadata. Use this when exact brand font rendering is required.",
    {
      background_image_url: z.string().optional().describe("Optional public URL, GoClaw /v1/files path, MEDIA: path, or local file path"),
      canvas_width: z.number().int().min(128).max(4096),
      canvas_height: z.number().int().min(128).max(4096),
      background_color: z.string().optional().describe("Canvas color when background_image_url is omitted"),
      text_layers: z.array(textLayerSchema).min(1),
      output_format: z.enum(["png", "jpeg"]).optional(),
      filename: z.string().optional(),
      page_id: z.string().optional(),
    },
    async ({ background_image_url, canvas_width, canvas_height, background_color, text_layers, output_format, filename, page_id }) => {
      try {
        const client = registry.getClient(page_id);
        const background = background_image_url ? await client.loadAssetBuffer(background_image_url) : undefined;
        const result = await renderCreative(
          {
            canvas_width,
            canvas_height,
            background_color,
            background_image: background?.buffer,
            text_layers,
            output_format,
          },
          async (fontPath) => (await client.loadAssetBuffer(fontPath)).buffer,
        );
        const data = result.data.toString("base64");
        const outputName = filename || `creative.${output_format === "jpeg" ? "jpg" : "png"}`;
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({
                page_id: client.getPageId(),
                filename: outputName,
                content_type: result.contentType,
                size_bytes: result.data.length,
                canvas_width,
                canvas_height,
                rendered_by: "fb_render_creative",
                fonts: result.fontMeta,
              }, null, 2),
            },
            {
              type: "image" as const,
              data,
              mimeType: result.contentType,
            },
          ],
        };
      } catch (e) {
        return errorResult(e, page_id);
      }
    },
  );
}
