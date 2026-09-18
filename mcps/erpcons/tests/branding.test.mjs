import assert from "node:assert/strict";
import test from "node:test";

import { resolveCompany } from "../src/branding.ts";
import { ErpClient, ToolInputError } from "../src/erp-client.ts";

const companies = {
  items: [
    { id: 1686, name: "Công ty Cổ phần Công nghệ máy tính Tekshot", abbreviation: "TKS", hasLogo: true },
    { id: 1683, name: "Công ty TNHH Công nghệ thực phẩm Foodtek", abbreviation: "", hasLogo: true },
    { id: 1959, name: "Phân xưởng sản xuất Foodtek", abbreviation: "", hasLogo: true },
  ],
  default: 1683,
};
const client = { fetchBranding: async () => companies };

test("resolveCompany uses the caller's main company when none is named", async () => {
  assert.equal(await resolveCompany(client, undefined), 1683);
  assert.equal(await resolveCompany(client, "  "), 1683);
});

test("resolveCompany accepts IDs and names or abbreviations, without diacritics", async () => {
  assert.equal(await resolveCompany(client, 1686), 1686);
  assert.equal(await resolveCompany(client, "1686"), 1686);
  assert.equal(await resolveCompany(client, "tekshot"), 1686);
  assert.equal(await resolveCompany(client, "TKS"), 1686);
  assert.equal(await resolveCompany(client, "phan xuong foodtek"), 1959);
});

test("resolveCompany refuses to guess between several companies or outside the scope", async () => {
  await assert.rejects(resolveCompany(client, "Foodtek"), (e) => e instanceof ToolInputError && /nhiều công ty/.test(e.message));
  await assert.rejects(resolveCompany(client, "Buildtek"), (e) => e instanceof ToolInputError && /phạm vi/.test(e.message));
});

test("resolveCompany asks back when the caller has no main company and several choices", async () => {
  const noDefault = { fetchBranding: async () => ({ ...companies, default: null }) };
  await assert.rejects(resolveCompany(noDefault, undefined), /Hỏi lại/);
});

test("fetchSiteImage only follows same-origin relative paths", async () => {
  const erp = new ErpClient("http://erp.example/api/v1", "secret", 1000);
  for (const path of ["http://evil.example/x.png", "//evil.example/x.png", "/sites/../../etc/passwd", "/a%2F..%2F..%2Fb", "/x.png?y=1", "files/x.png"]) {
    await assert.rejects(erp.fetchSiteImage(path, 1024), (e) => e instanceof ToolInputError, path);
  }
});
