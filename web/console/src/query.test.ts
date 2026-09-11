import { describe, expect, test } from "vitest";
import { queryClient, switchTenant } from "./query";
import { scopedController } from "./api/abort";
import { useScope } from "./state/scope";
describe("tenant switching", () => {
  test("clears tenant-scoped query and temporary state", async () => {
    queryClient.setQueryData(["tenant", "a"], { id: "a" });
    useScope.getState().setProject("project-a");
    useScope.getState().setDraft("draft-a");
    await switchTenant("tenant-b");
    expect(queryClient.getQueryData(["tenant", "a"])).toBeUndefined();
    expect(useScope.getState()).toMatchObject({
      tenantId: "tenant-b",
      projectId: "",
      draftId: "",
    });
  });
  test("aborts scoped SSE/fetch controllers", async () => {
    const controller = scopedController();
    expect(controller.signal.aborted).toBe(false);
    await switchTenant("tenant-c");
    expect(controller.signal.aborted).toBe(true);
  });
});
