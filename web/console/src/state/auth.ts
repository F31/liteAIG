import { create } from "zustand";
export type AuthState = {
  username: string;
  role: string;
  scopes: string[];
  authMethod: string;
  setAuth: (value: {
    username?: string;
    role?: string;
    scopes?: string[];
    authMethod?: string;
  }) => void;
  resetAuth: () => void;
};
export const useAuth = create<AuthState>((set) => ({
  username: "",
  role: "",
  scopes: [],
  authMethod: "",
  setAuth: (value) => set(value),
  resetAuth: () => set({ username: "", role: "", scopes: [], authMethod: "" }),
}));
export const isAdminRole = (role: string) => role === "tenant_admin";
export const hasPermission = (scopes: string[], permission: string) =>
  scopes.includes(permission);
