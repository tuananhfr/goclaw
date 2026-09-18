import assert from "node:assert/strict";
import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { intentKey, RouteMemory } from "../src/route-memory.ts";

const DAY = 86_400_000;
const all = new Set(["dplan_reports", "ontime_reports", "projects"]);

test("intentKey drops filler words, dates and diacritics", () => {
  assert.equal(intentKey("Cho tôi báo cáo t8"), "bao cao");
  assert.equal(intentKey("báo cáo tháng 9 năm 2026"), "bao cao");
  assert.equal(intentKey("   "), "");
});

test("a recorded route is suggested for a differently dated question", () => {
  const memory = new RouteMemory();
  memory.record("u1", "báo cáo t8", "dplan_reports");
  const [top] = memory.suggest("u1", "cho tôi báo cáo tháng 9", all);
  assert.equal(top.capability, "dplan_reports");
});

test("one person's habits never reach another person", () => {
  const memory = new RouteMemory();
  memory.record("other", "báo cáo của Trần Văn A", "ontime_reports");
  memory.record("u1", "báo cáo", "dplan_reports");
  assert.deepEqual(memory.suggest("u1", "báo cáo t8", all).map((s) => s.capability), ["dplan_reports"]);
  assert.deepEqual(memory.suggest("u2", "báo cáo t8", all), []);
  assert.deepEqual(memory.favorites("u2", all), []);
});

test("suggestions never include capabilities outside the caller's catalog", () => {
  const memory = new RouteMemory();
  memory.record("u1", "báo cáo", "dplan_reports");
  assert.deepEqual(memory.suggest("u1", "báo cáo", new Set(["projects"])), []);
  assert.deepEqual(memory.favorites("u1", new Set(["projects"])), []);
});

test("unrelated questions get no suggestion", () => {
  const memory = new RouteMemory();
  memory.record("u1", "báo cáo dplan", "dplan_reports");
  assert.deepEqual(memory.suggest("u1", "danh sách dự án", all), []);
});

test("old habits decay behind recent ones", () => {
  let now = 0;
  const memory = new RouteMemory("", () => now);
  for (let i = 0; i < 3; i++) memory.record("u1", "báo cáo", "ontime_reports");
  now = 120 * DAY;
  memory.record("u1", "báo cáo", "dplan_reports");
  assert.equal(memory.suggest("u1", "báo cáo", all)[0].capability, "dplan_reports");
});

test("favorites group wordings by capability and show the latest wording", () => {
  let now = 0;
  const memory = new RouteMemory("", () => now);
  memory.record("u1", "báo cáo tháng 8", "dplan_reports");
  memory.record("u1", "dự án đang chạy", "projects");
  now = 1_000;
  memory.record("u1", "dự án của tôi", "projects");
  assert.deepEqual(memory.favorites("u1", all), [
    { intent: "dự án của tôi", capability: "projects" },
    { intent: "báo cáo tháng 8", capability: "dplan_reports" },
  ]);
});

test("memory survives a restart through its file", () => {
  const file = join(mkdtempSync(join(tmpdir(), "route-memory-")), "nested", "memory.json");
  const first = new RouteMemory(file);
  first.record("u1", "báo cáo", "dplan_reports");
  first.flush();
  assert.match(readFileSync(file, "utf8"), /dplan_reports/);

  const second = new RouteMemory(file);
  assert.equal(second.suggest("u1", "báo cáo", all)[0].capability, "dplan_reports");
});
