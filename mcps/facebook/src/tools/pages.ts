/**
 * MCP Tool registrations — Pages domain.
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { PageRegistry } from "../page-registry.js";

function ok(data: unknown, label: string) {
  return { content: [{ type: "text" as const, text: `✅ ${label}\n\n${JSON.stringify(data, null, 2)}` }] };
}

function errorResult(err: unknown) {
  return { content: [{ type: "text" as const, text: `❌ Error: ${err instanceof Error ? err.message : String(err)}` }], isError: true };
}

export function registerPageTools(server: McpServer, registry: PageRegistry) {
  server.tool(
    "fb_add_page",
    "Register a new Facebook Page with an access token. This validates the token and adds it to the session registry.",
    {
      page_id: z.string().describe("The ID of the Facebook Page"),
      access_token: z.string().describe("The Page Access Token"),
      name: z.string().optional().describe("Optional friendly name for the page"),
    },
    async ({ page_id, access_token, name }) => {
      try {
        const config = await registry.addPage(page_id, access_token, name);
        return ok({ page_id: config.pageId, name: config.name }, `Page registered successfully`);
      } catch (e) { return errorResult(e); }
    },
  );

  server.tool(
    "fb_list_pages",
    "List all registered Facebook Pages in the current session.",
    {},
    async () => {
      const pages = registry.listPages();
      return ok({ count: pages.length, pages }, `Found ${pages.length} registered pages`);
    },
  );

  server.tool(
    "fb_remove_page",
    "Remove a registered Facebook Page from the current session.",
    {
      page_id: z.string().describe("The ID of the Facebook Page to remove"),
    },
    async ({ page_id }) => {
      const removed = registry.removePage(page_id);
      if (removed) {
        return ok({ success: true }, `Page ${page_id} removed from registry`);
      }
      return errorResult(`Page ${page_id} not found in registry`);
    },
  );

  server.tool(
    "fb_set_default_page",
    "Set the default Facebook Page to be used when page_id is omitted in other tool calls.",
    {
      page_id: z.string().describe("The ID of the Facebook Page to set as default"),
    },
    async ({ page_id }) => {
      try {
        registry.setDefaultPage(page_id);
        return ok({ success: true, default_page_id: page_id }, `Default page set to ${page_id}`);
      } catch (e) { return errorResult(e); }
    },
  );
}
