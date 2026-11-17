import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../../lib/api";
import type { Product } from "../../types";
import PlatformShell from "./PlatformShell";

interface FormState {
  sku: string;
  slug: string;
  name: string;
  genericName: string;
  strength: string;
  dosageForm: string;
  packSize: string;
  description: string;
  mrpRupees: string;
  priceRupees: string;
  requiresPrescription: boolean;
  maxQtyPerOrder: string;
  isActive: boolean;
  isFeatured: boolean;
}

const EMPTY: FormState = {
  sku: "", slug: "", name: "", genericName: "", strength: "", dosageForm: "", packSize: "",
  description: "", mrpRupees: "", priceRupees: "", requiresPrescription: false,
  maxQtyPerOrder: "10", isActive: true, isFeatured: false,
};

function fromProduct(p: Product): FormState {
  return {
    sku: p.sku, slug: p.slug, name: p.name, genericName: p.genericName ?? "",
    strength: p.strength ?? "", dosageForm: p.dosageForm ?? "", packSize: p.packSize ?? "",
    description: p.description ?? "", mrpRupees: (p.mrpPaise / 100).toString(),
    priceRupees: (p.pricePaise / 100).toString(), requiresPrescription: p.requiresPrescription,
    maxQtyPerOrder: p.maxQtyPerOrder.toString(), isActive: p.isActive, isFeatured: p.isFeatured,
  };
}

export default function PlatformProductForm() {
  const { id } = useParams();
  const isNew = !id || id === "new";
  const navigate = useNavigate();
  const qc = useQueryClient();

  const [form, setForm] = useState<FormState>(EMPTY);
  const [error, setError] = useState<string | null>(null);

  const { data, isError, error: loadError } = useQuery({
    queryKey: ["platform-product", id],
    queryFn: () => api.get<Envelope<Product>>(`/api/v1/platform/products/${id}`),
    enabled: !isNew,
    retry: false,
  });

  useEffect(() => {
    if (data) setForm(fromProduct(data.data));
  }, [data]);

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  const save = useMutation({
    mutationFn: () => {
      const body = {
        sku: form.sku, slug: form.slug, name: form.name,
        genericName: form.genericName || null, strength: form.strength || null,
        dosageForm: form.dosageForm || null, packSize: form.packSize || null,
        description: form.description || null,
        mrpPaise: Math.round(parseFloat(form.mrpRupees || "0") * 100),
        pricePaise: Math.round(parseFloat(form.priceRupees || "0") * 100),
        requiresPrescription: form.requiresPrescription,
        maxQtyPerOrder: parseInt(form.maxQtyPerOrder || "1", 10),
        isActive: form.isActive, isFeatured: form.isFeatured,
      };
      return isNew
        ? api.post<Envelope<Product>>("/api/v1/platform/products", body)
        : api.patch<Envelope<Product>>(`/api/v1/platform/products/${id}`, body);
    },
    onSuccess: (res) => {
      setError(null);
      if (isNew) {
        navigate(`/platform/products/${res.data.id}`);
      } else {
        qc.setQueryData(["platform-product", id], res);
      }
    },
    onError: (e) => setError(e instanceof ApiError ? e.message : "Save failed."),
  });

  if (isError && loadError instanceof ApiError && loadError.status === 401) {
    navigate("/platform");
    return null;
  }

  return (
    <PlatformShell>
      <div className="max-w-3xl mx-auto px-6 py-8">
        <h1 className="text-lg font-bold text-slate-900 mb-1">{isNew ? "New master product" : "Edit master product"}</h1>
        <p className="text-sm text-slate-500 mb-5">
          These fields are shared by every business that lists this product. Price here is only the default —
          each business can override it from their own Catalog screen.
        </p>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
          className="bg-white border border-slate-200 rounded-2xl p-5 space-y-4"
        >
          {error && <p className="text-sm text-red-600">{error}</p>}

          <div className="grid grid-cols-2 gap-3">
            <Field label="Name">
              <input className="input text-sm" value={form.name} onChange={(e) => set("name", e.target.value)} required />
            </Field>
            <Field label="SKU">
              <input className="input text-sm font-mono" value={form.sku} onChange={(e) => set("sku", e.target.value)} required />
            </Field>
            <Field label="Slug (URL)">
              <input className="input text-sm font-mono" value={form.slug} onChange={(e) => set("slug", e.target.value)} required />
            </Field>
            <Field label="Generic name">
              <input className="input text-sm" value={form.genericName} onChange={(e) => set("genericName", e.target.value)} />
            </Field>
            <Field label="Strength">
              <input className="input text-sm" value={form.strength} onChange={(e) => set("strength", e.target.value)} placeholder="500 mg" />
            </Field>
            <Field label="Pack size">
              <input className="input text-sm" value={form.packSize} onChange={(e) => set("packSize", e.target.value)} placeholder="Strip of 10" />
            </Field>
            <Field label="MRP (₹)">
              <input className="input text-sm" type="number" step="0.01" value={form.mrpRupees} onChange={(e) => set("mrpRupees", e.target.value)} required />
            </Field>
            <Field label="Default selling price (₹)">
              <input className="input text-sm" type="number" step="0.01" value={form.priceRupees} onChange={(e) => set("priceRupees", e.target.value)} required />
            </Field>
            <Field label="Max qty per order">
              <input className="input text-sm" type="number" min={1} value={form.maxQtyPerOrder} onChange={(e) => set("maxQtyPerOrder", e.target.value)} required />
            </Field>
          </div>

          <Field label="Description">
            <textarea className="input text-sm" rows={3} value={form.description} onChange={(e) => set("description", e.target.value)} />
          </Field>

          <div className="flex flex-wrap gap-5 text-sm pt-1">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={form.requiresPrescription} onChange={(e) => set("requiresPrescription", e.target.checked)} />
              Requires prescription
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={form.isFeatured} onChange={(e) => set("isFeatured", e.target.checked)} />
              Featured by default
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={form.isActive} onChange={(e) => set("isActive", e.target.checked)} />
              Active (businesses may list it)
            </label>
          </div>

          <button
            type="submit"
            disabled={save.isPending}
            className="bg-brand-600 text-white text-sm font-semibold rounded-lg px-5 py-2.5 disabled:opacity-50"
          >
            {save.isPending ? "Saving…" : isNew ? "Create product" : "Save changes"}
          </button>
        </form>
      </div>
    </PlatformShell>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-slate-500 mb-1">{label}</span>
      {children}
    </label>
  );
}
