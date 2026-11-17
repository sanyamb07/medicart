// Single source for cart reads and writes so the header badge, product cards,
// product page, cart and checkout all share one cache entry and one set of
// behaviours. Every write goes through the existing cart endpoints and the
// server's response replaces the cache — the server stays the authority on
// quantities, limits and stock.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../lib/api";
import type { CartSummary } from "../types";
import { track } from "../lib/analytics";
import { useToast } from "../components/Toast";

export function useCart() {
  const query = useQuery({
    queryKey: ["cart"],
    queryFn: () => api.get<Envelope<CartSummary>>("/api/v1/cart"),
  });
  const cart = query.data?.data;
  const qtyOf = (productId: string) => cart?.lines.find((l) => l.productId === productId)?.qty ?? 0;
  return { cart, isLoading: query.isLoading, isError: query.isError, qtyOf };
}

interface AddInput {
  productId: string;
  qty: number;
  pricePaise: number;
  name?: string;
}

export function useAddToCart(opts: { silent?: boolean } = {}) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: ({ productId, qty }: AddInput) =>
      api.post<Envelope<CartSummary>>("/api/v1/cart/items", { productId, qty }),
    onSuccess: (res, v) => {
      qc.setQueryData(["cart"], res);
      track("add_to_cart", { productId: v.productId, pricePaise: v.pricePaise, qty: v.qty });
      if (!opts.silent) toast({ tone: "success", title: "Added to cart", action: { label: "View cart", to: "/cart" } });
    },
    onError: (err) => {
      toast({ tone: "error", title: err instanceof ApiError ? err.message : "Couldn't add to cart. Please try again." });
    },
  });
}

export function useSetCartQty() {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: ({ productId, qty }: { productId: string; qty: number }) =>
      qty <= 0
        ? api.delete<Envelope<CartSummary>>(`/api/v1/cart/items/${productId}`)
        : api.patch<Envelope<CartSummary>>(`/api/v1/cart/items/${productId}`, { qty }),
    onSuccess: (res) => qc.setQueryData(["cart"], res),
    onError: (err) => {
      toast({ tone: "error", title: err instanceof ApiError ? err.message : "Couldn't update quantity." });
    },
  });
}

/** MRP total and discount for display. The server's subtotal stays authoritative. */
export function cartTotals(cart: CartSummary) {
  const mrp = cart.lines.reduce((s, l) => s + l.unitMrpPaise * l.qty, 0);
  return { mrp, discount: Math.max(0, mrp - cart.subtotalPaise) };
}
