import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ApiError, api } from "./api";
import { type MetricsRange, type MetricsResponse, mockMetrics } from "./metrics";

export type MetricsState =
  | { status: "loading" }
  | { status: "unavailable" }
  | { status: "error"; message: string }
  | { status: "ready"; data: MetricsResponse; refreshing: boolean };

interface Options {
  org: string;
  name: string;
  range: MetricsRange;
  enabled: boolean;
  mock: boolean;
  limits: { cpu?: number; memory?: number };
}

export function useServerMetrics({ org, name, range, enabled, mock, limits }: Options): MetricsState {
  const query = useQuery({
    queryKey: ["metrics", org, name, range, mock],
    queryFn: () => (mock ? Promise.resolve(mockMetrics(range, limits)) : api.getMetrics(org, name, range)),
    enabled,
    refetchInterval: 10_000,
    placeholderData: keepPreviousData,
  });

  if (query.data) return { status: "ready", data: query.data, refreshing: query.isFetching };
  if (query.isError) {
    const err = query.error;
    if (err instanceof ApiError && (err.status === 404 || err.status === 501)) return { status: "unavailable" };
    return { status: "error", message: err instanceof Error ? err.message : String(err) };
  }
  return { status: "loading" };
}
