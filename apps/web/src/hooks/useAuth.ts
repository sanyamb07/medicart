import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../lib/api";

export interface CustomerUser {
  id: string;
  mobile: string | null;
  fullName: string | null;
  customerId: string | null;
}

// Resolves the signed-in customer, if any. A 401 is the expected "not signed
// in" response, not an error worth retrying or surfacing — react-query's
// default retry is disabled for this query for exactly that reason.
export function useAuth() {
  const query = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api.get<Envelope<CustomerUser>>("/api/v1/auth/me");
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return null;
        throw err;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
  return { user: query.data?.data ?? null, isLoading: query.isLoading };
}

export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api.post("/api/v1/auth/logout");
    qc.setQueryData(["me"], null);
  };
}
