import assert from "node:assert/strict";
import test from "node:test";

import {
  buildGetDescription,
  callCapability,
  findCapabilities,
  hasData,
  tokenize,
} from "../src/mcp-server.ts";
import { addReadableDates, ErpClient, ToolInputError } from "../src/erp-client.ts";

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

const vietnameseCatalog = {
  route_logistics: {
    label: "Phiếu xuất kho (logistics)",
    domain: "orders",
    kind: "list",
    description: "Danh sách phiếu xuất kho, vận chuyển hàng. Dùng cho \"phiếu xuất kho tháng này\".",
    path: "/logistics",
    params: {},
  },
  route_overtime: {
    label: "Đề xuất làm thêm giờ",
    domain: "proposals",
    kind: "list",
    description: "Danh sách đề xuất làm thêm giờ (OT).",
    path: "/propose/suggest-overtime",
    params: {},
  },
  route_voucher: {
    label: "Phiếu thu chi",
    domain: "finance",
    kind: "list",
    description: "Phiếu thu, phiếu chi của công ty.",
    path: "/finance/vouchers",
    params: {},
  },
};

test("findCapabilities matches Vietnamese queries without diacritics and extra words", () => {
  const found = findCapabilities(vietnameseCatalog, { query: "cho tôi danh sách phieu xuat kho thang 8 cua toi" });
  assert.equal(found[0].key, "route_logistics");
});

test("findCapabilities ranks the two-syllable phrase above a shared single word", () => {
  // "Đề xuất" và "Phiếu thu chi" đều khớp một âm tiết, nhưng phải xếp sau.
  const found = findCapabilities(vietnameseCatalog, { query: "phiếu xuất kho" });
  assert.equal(found[0].key, "route_logistics");
  assert.equal(found.length, 3);
});

test("findCapabilities falls back to every domain when the guessed one has nothing", () => {
  const found = findCapabilities(vietnameseCatalog, { query: "làm thêm giờ", domain: "hr" });
  assert.deepEqual(found.map((item) => item.key), ["route_overtime"]);
});

test("findCapabilities keeps business words that look like stopwords once folded", () => {
  assert.deepEqual(tokenize("Bán hàng thẻ"), ["ban", "hang", "the"]);
  assert.deepEqual(tokenize("Đơn của tôi"), ["don"]);
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

test("findCapabilities treats a wrong domain guess as a hint, not a filter", () => {
  // "hr" có capability khác, nên kiểu nới-khi-rỗng cũ vẫn trả về rỗng.
  const mixed = { ...vietnameseCatalog, employees_list: catalog.employees_list };
  const found = findCapabilities(mixed, { query: "đề xuất làm thêm giờ", domain: "hr" });
  assert.equal(found[0].key, "route_overtime");
});

test("findCapabilities ranks learned routes first and flags them", () => {
  const found = findCapabilities(vietnameseCatalog, { query: "phiếu" }, [
    { capability: "route_voucher", score: 3 },
  ]);
  assert.equal(found[0].key, "route_voucher");
  assert.equal(found[0].learned, true);
  assert.equal(found[1].learned, undefined);
});

test("findCapabilities pairs a list with its detail capability by path", () => {
  const paired = {
    projects: { label: "Dự án", domain: "projects", kind: "list", description: "", path: "/projects", params: {} },
    project_detail: {
      label: "Chi tiết dự án", domain: "projects", kind: "detail", description: "", path: "/projects/{node}",
      params: { node: { type: "integer", description: "ID", required: true, in: "path" } },
    },
    project_board: { label: "Tổng quan dự án", domain: "projects", kind: "report", description: "", path: "/projects/board", params: {} },
  };
  const found = findCapabilities(paired, { query: "dự án" });
  assert.equal(found.find((item) => item.key === "projects").detail, "project_detail");
  assert.equal(found.find((item) => item.key === "project_board").detail, undefined);
});

test("buildGetDescription lists the per-user catalog without lookups and puts guidance first", () => {
  const text = buildGetDescription({
    ...catalog,
    customer_lookup: { label: "Chọn khách hàng", domain: "crm", kind: "lookup", description: "", path: "/customer/options" },
  }, [{ intent: "báo cáo tháng 8", capability: "projects_list" }]);

  assert.match(text.slice(0, 200), /MỤC LỤC/);
  assert.match(text, /projects_list — Project list \(q\)/);
  assert.match(text, /\[hr\]\nemployees_list — Employee list/);
  assert.match(text, /- báo cáo tháng 8 -> projects_list/);
  assert.doesNotMatch(text, /customer_lookup/);
});

test("hasData tells real rows from empty shells", () => {
  assert.equal(hasData({ items: [], total: 0 }), false);
  assert.equal(hasData({ status: "success", data: [] }), false);
  assert.equal(hasData({ items: [{ id: 1 }] }), true);
  assert.equal(hasData({ data: { id: 6 } }), true);
});

test("addReadableDates adds GMT+7 text next to second timestamps only", () => {
  const data = {
    items: [{ id: 5, created: 1754097437, changed: "1755083305", updatedAt: 1754102081, amount: 1754097437 }],
    total: 1,
  };
  addReadableDates(data, 0);
  const row = data.items[0];
  assert.equal(row.created_text, "02/08/2025 08:17");
  assert.equal(row.changed_text, "13/08/2025 18:08");
  assert.equal(row.updatedAt_text, "02/08/2025 09:34");
  assert.equal(row.created, 1754097437);
  assert.equal(row.amount_text, undefined);
  assert.equal(data.total_text, undefined);
});
