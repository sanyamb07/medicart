import { useQuery } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../lib/api";
import type { ServiceabilityArea } from "../types";
import { useDeliveryPin } from "../lib/prefs";

/** Live serviceability for a PIN (cached per PIN, shared app-wide). */
export function useServiceability(pin: string) {
  return useQuery({
    queryKey: ["serviceability", pin],
    queryFn: async () => {
      try {
        return (await api.get<Envelope<ServiceabilityArea>>(`/api/v1/serviceability/${pin}`)).data;
      } catch (e) {
        // An unknown PIN is a normal answer ("we don't deliver there"), not a failure.
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }
    },
    enabled: /^\d{6}$/.test(pin),
    staleTime: 10 * 60_000,
  });
}

/** The shopper's saved PIN plus its serviceability. */
export function useDeliveryEstimate() {
  const [pin, setPin] = useDeliveryPin();
  const { data: area, isLoading } = useServiceability(pin);
  const eta =
    area?.isServiceable && area.etaMinDays != null
      ? area.etaMinDays === area.etaMaxDays
        ? `${area.etaMinDays} day${area.etaMinDays === 1 ? "" : "s"}`
        : `${area.etaMinDays}–${area.etaMaxDays} days`
      : null;
  return { pin, setPin, area, eta, isLoading: isLoading && /^\d{6}$/.test(pin) };
}
