import { MutationCache, QueryClient } from "@tanstack/react-query";
import { message } from "antd";
import { abortTenantRequests } from "./api/abort";
import { APIError } from "./api/client";
import i18n from "./i18n";
import { useScope } from "./state/scope";
const mutationCache = new MutationCache({
  onError: (error, _variables, _context, mutation) => {
    if (mutation.options.onError) return;
    if (error instanceof APIError) {
      void message.error(i18n.t("common.error"));
    }
  },
});
export const queryClient = new QueryClient({
  mutationCache,
  defaultOptions: {
    queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});
export async function switchTenant(tenantId: string) {
  await queryClient.cancelQueries();
  abortTenantRequests();
  queryClient.clear();
  useScope.getState().resetTenantState();
  useScope.getState().setTenant(tenantId);
}
