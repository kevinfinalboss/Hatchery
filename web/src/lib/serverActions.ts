import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";

export function useRestartServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ org, name }: { org: string; name: string }) => api.restartGameServer(org, name),
    onSuccess: (_data, { org, name }) => {
      void queryClient.invalidateQueries({ queryKey: ["gameserver", org, name] });
      void queryClient.invalidateQueries({ queryKey: ["gameservers", org] });
    },
  });
}

export function useSetServerState() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ org, name, state }: { org: string; name: string; state: "Running" | "Stopped" }) =>
      api.setGameServerState(org, name, state),
    onSuccess: (_data, { org, name }) => {
      void queryClient.invalidateQueries({ queryKey: ["gameserver", org, name] });
      void queryClient.invalidateQueries({ queryKey: ["gameservers", org] });
    },
  });
}
