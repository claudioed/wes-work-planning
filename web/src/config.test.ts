import { describe, expect, it } from "vitest";
import { resolveWesApiBase } from "./config";

describe("resolveWesApiBase", () => {
  it("builds the production API base from the runtime API origin", () => {
    expect(resolveWesApiBase({ apiOrigin: "http://localhost:8000" }, true)).toBe(
      "http://localhost:8000/api/wes-work-planning",
    );
  });

  it("normalizes a trailing slash on the runtime API origin", () => {
    expect(resolveWesApiBase({ apiOrigin: "https://warehouse.example/" }, true)).toBe(
      "https://warehouse.example/api/wes-work-planning",
    );
  });

  it("fails loudly when production runtime configuration has no API origin", () => {
    expect(() => resolveWesApiBase({}, true)).toThrow(
      "window.__WAREHOUSE_CONFIG__.apiOrigin is required in production",
    );
  });

  it("retains the existing standalone API origin in Vite development", () => {
    expect(resolveWesApiBase({}, false)).toBe("http://localhost:8083");
  });
});
