import { create } from "zustand";
type ScopeState = {
  tenantId: string;
  projectId: string;
  draftId: string;
  setTenant: (id: string) => void;
  setProject: (id: string) => void;
  setDraft: (id: string) => void;
  resetTenantState: () => void;
};
export const useScope = create<ScopeState>((set) => ({
  tenantId: "",
  projectId: "",
  draftId: "",
  setTenant: (tenantId) => set({ tenantId }),
  setProject: (projectId) => set({ projectId }),
  setDraft: (draftId) => set({ draftId }),
  resetTenantState: () => set({ projectId: "", draftId: "" }),
}));
