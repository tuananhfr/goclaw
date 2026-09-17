import assert from "node:assert/strict";
import test from "node:test";

import {
  callCapability,
  findCapabilities,
} from "../src/mcp-server.ts";
import { ErpClient, ToolInputError } from "../src/erp-client.ts";

const catalog = {
  projects_list: {
    label: "Project list",
    domain: "projects",
    kind: "list",
    description: "Find projects visible to the current user.",
    path: "/projects",
    params: {
      q: {
        type: "string",
        description: "Từ khoá tên dự án",
        required: false,
        in: "query",
      },
    },
  },
  employees_list: {
    label: "Employee list",
    domain: "hr",
    kind: "list",
    description: "Find employees visible to the current user.",
    path: "/hr/employees",
    params: {},
  },
};

test("findCapabilities searches only the supplied per-user catalog", () => {
  assert.deepEqual(
    findCapabilities(catalog, { query: "project", limit: 10 }),
    [{
      key: "projects_list",
      label: "Project list",
      domain: "projects",
      kind: "list",
      description: "Find projects visible to the current user.",
      requiredParams: [],
      optionalParams: ["q"],
      params: catalog.projects_list.params,
    }],
  );
});

test("findCapabilities filters by domain and kind", () => {
  const found = findCapabilities(catalog, { domain: "hr", kind: "list" });
  assert.deepEqual(found.map((item) => item.key), ["employees_list"]);
});

test("callCapability dispatches a catalog entry by key", async () => {
  const calls = [];
  const client = {
    async callTool(tool, args) {
      calls.push({ tool, args });
      return { items: [{ id: 1 }] };
    },
  };

  const result = await callCapability(catalog, client, "projects_list", { limit: 1 });
  assert.deepEqual(result, { items: [{ id: 1 }] });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].tool.path, "/projects");
});

test("callCapability rejects keys outside the per-user catalog", async () => {
  const client = { callTool: async () => assert.fail("must not dispatch") };
  await assert.rejects(
    callCapability(catalog, client, "../../admin", {}),
    /không có trong danh mục/i,
  );
});

test("ErpClient rejects undeclared parameters before sending a request", async () => {
  const client = new ErpClient("https://erp.example/api/v1", "secret", 1000);

  await assert.rejects(
    () => client.callTool(catalog.projects_list, { uid: 99 }),
    (error) => error instanceof ToolInputError && /uid/.test(error.message),
  );
});

test("ErpClient rejects unsafe catalog paths", async () => {
  const client = new ErpClient("https://erp.example/api/v1", "secret", 1000);

  await assert.rejects(
    () => client.callTool({ ...catalog.projects_list, path: "https://evil.example/steal" }, {}),
    (error) => error instanceof ToolInputError && /đường dẫn/i.test(error.message),
  );
});

test("ErpClient validates catalog parameter types and enums", async () => {
  const client = new ErpClient("https://erp.example/api/v1", "secret", 1000);
  const tool = {
    ...catalog.projects_list,
    params: {
      limit: { type: "integer", description: "Limit", required: false, in: "query" },
      scope: {
        type: "string",
        description: "Scope",
        required: false,
        in: "query",
        enum: ["mine", "all"],
      },
    },
  };

  await assert.rejects(
    () => client.callTool(tool, { limit: "many" }),
    (error) => error instanceof ToolInputError && /limit/.test(error.message),
  );
  await assert.rejects(
    () => client.callTool(tool, { scope: "everyone" }),
    (error) => error instanceof ToolInputError && /scope/.test(error.message),
  );
});
