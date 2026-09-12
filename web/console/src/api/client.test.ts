import { afterEach, describe, expect, test, vi } from "vitest";
import {
  APIError,
  UNAUTHORIZED_EVENT,
  api,
  clearCSRFToken,
  setCSRFToken,
} from "./client";

describe("API re-authentication failures", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    clearCSRFToken();
  });

  test("does not turn REAUTH_REQUIRED into a global logout", async () => {
    setCSRFToken("csrf");
    const unauthorized = vi.fn();
    window.addEventListener(UNAUTHORIZED_EVENT, unauthorized);
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({ error: { code: "REAUTH_REQUIRED" } }),
      }),
    );

    await expect(
      api("/api/admin/keys/key-1/revoke", { method: "POST" }),
    ).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({ code: "REAUTH_REQUIRED" }),
    );
    expect(unauthorized).not.toHaveBeenCalled();

    window.removeEventListener(UNAUTHORIZED_EVENT, unauthorized);
  });
});
