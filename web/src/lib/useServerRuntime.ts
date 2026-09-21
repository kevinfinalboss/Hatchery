import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

export function useServerRuntime(org: string, name: string, phase: string | undefined) {
  return useQuery({
    queryKey: ["runtime", org, name, phase],
    queryFn: () => api.getRuntime(org, name),
    refetchInterval: 10_000,
  });
}
