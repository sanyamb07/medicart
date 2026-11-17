import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { CheckCircle2, ImageOff, Search, X } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import type {
  EnrichmentStatus,
  ImageCandidate,
  ImageResearchStatus,
  MedicineInfo,
  VerificationStatus,
} from "../../types";

const RESEARCH_LABEL: Record<ImageResearchStatus, string> = {
  NOT_FOUND: "No genuine image found",
  RESEARCH_PENDING: "Research pending",
  IMAGE_FOUND: "Image found",
  QA_PENDING: "QA pending",
  QA_PASSED: "QA passed",
  PHARMACIST_REVIEW_PENDING: "Awaiting pharmacist",
  APPROVED: "Approved",
  REJECTED: "Rejected",
  MANUAL_VERIFICATION_REQUIRED: "Manual verification",
};

// Pharmacist decisions on one image candidate. Only "approve" shows it to customers.
const DECISIONS: { key: string; label: string; style: string }[] = [
  { key: "APPROVE", label: "Approve: matches brand, strength, form", style: "bg-green-600 text-white" },
  { key: "REJECTED", label: "Reject", style: "border border-red-300 text-red-700" },
  { key: "REQUEST_BETTER_IMAGE", label: "Request better image", style: "border border-slate-300" },
  { key: "WRONG_MANUFACTURER", label: "Wrong manufacturer", style: "border border-slate-300" },
  { key: "WRONG_MEDICINE", label: "Wrong medicine", style: "border border-slate-300" },
  { key: "MANUAL_VERIFICATION", label: "Needs manual verification", style: "border border-slate-300" },
];
import PlatformShell from "./PlatformShell";

interface Summary {
  total: number;
  byStatus: Partial<Record<EnrichmentStatus, number>>;
  withDescription: number;
  verifiedImages: number;
  placeholderImages: number;
  noImage: number;
  humanVerified: number;
  byVerification?: Partial<Record<VerificationStatus, number>>;
  pendingImages?: number;
  rejectedImages?: number;
}

const VERIFY_LABEL: Record<VerificationStatus, string> = {
  VERIFIED: "Verified",
  PARTIALLY_VERIFIED: "Partially verified",
  NEEDS_REVIEW: "Needs review",
};
const VERIFY_STYLE: Record<VerificationStatus, string> = {
  VERIFIED: "bg-green-50 text-green-700",
  PARTIALLY_VERIFIED: "bg-amber-50 text-amber-700",
  NEEDS_REVIEW: "bg-red-50 text-red-700",
};

const STATUS_LABEL: Record<EnrichmentStatus, string> = {
  pending: "Pending",
  processing: "Processing",
  partially_completed: "Partial",
  completed: "Complete",
  needs_manual_review: "Needs review",
  failed: "Failed",
};
const STATUS_STYLE: Record<EnrichmentStatus, string> = {
  pending: "bg-slate-100 text-slate-600",
  processing: "bg-blue-50 text-blue-700",
  partially_completed: "bg-amber-50 text-amber-700",
  completed: "bg-green-50 text-green-700",
  needs_manual_review: "bg-red-50 text-red-700",
  failed: "bg-red-100 text-red-800",
};
const IMAGE_LABEL = {
  placeholder: "Illustration",
  needs_review: "Photo awaiting review",
  verified: "Verified photo",
};

type Editable = Pick<
  MedicineInfo,
  | "genericName"
  | "uses"
  | "mechanismOfAction"
  | "precautions"
  | "sideEffects"
  | "warnings"
  | "contraindications"
  | "storageInfo"
  | "manufacturer"
>;
const FIELDS: { key: keyof Editable; label: string; rows: number }[] = [
  { key: "genericName", label: "Generic name / composition", rows: 1 },
  { key: "manufacturer", label: "Manufacturer", rows: 1 },
  { key: "uses", label: "Uses", rows: 4 },
  { key: "mechanismOfAction", label: "How it works", rows: 3 },
  { key: "precautions", label: "Precautions", rows: 4 },
  { key: "sideEffects", label: "Side effects", rows: 3 },
  { key: "warnings", label: "Important (boxed) warning", rows: 2 },
  { key: "contraindications", label: "Contraindications", rows: 2 },
  { key: "storageInfo", label: "Storage", rows: 2 },
];

// Plain names for the gaps the pipeline records.
const FIELD_LABEL: Record<string, string> = {
  product_information_from_supplier: "not a medicine: needs supplier info",
  mechanism_of_action: "how it works",
  side_effects: "side effects",
};

export default function PlatformMedicines() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [status, setStatus] = useState("");
  const [image, setImage] = useState("");
  const [verification, setVerification] = useState("");
  const [research, setResearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkResult, setBulkResult] = useState<{ approved: string[]; skipped: Record<string, string> } | null>(null);
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<MedicineInfo | null>(null);

  useEffect(() => {
    const t = setTimeout(() => {
      setQuery(q);
      setPage(1);
    }, 300);
    return () => clearTimeout(t);
  }, [q]);

  const summaryQ = useQuery({
    queryKey: ["enrichment-summary"],
    queryFn: () =>
      api.get<Envelope<Summary>>("/api/v1/platform/enrichment/summary"),
    retry: false,
  });
  const params = new URLSearchParams({ page: String(page), pageSize: "25" });
  if (status) params.set("status", status);
  if (image) params.set("image", image);
  if (verification) params.set("verification", verification);
  if (research) params.set("research", research);
  if (query) params.set("q", query);
  const listQ = useQuery({
    queryKey: ["enrichment-list", params.toString()],
    queryFn: () =>
      api.get<Envelope<MedicineInfo[]>>(
        `/api/v1/platform/enrichment/products?${params}`,
      ),
    enabled: !summaryQ.isError,
    placeholderData: keepPreviousData,
  });

  const bulk = useMutation({
    mutationFn: (productIds: string[]) =>
      api.post<Envelope<{ approved: string[]; skipped: Record<string, string> }>>(
        "/api/v1/platform/enrichment/images/bulk-approve",
        { productIds },
      ),
    onSuccess: (res) => {
      setBulkResult(res.data);
      setSelected(new Set());
      qc.invalidateQueries({ queryKey: ["enrichment-list"] });
      qc.invalidateQueries({ queryKey: ["enrichment-summary"] });
    },
  });

  if (
    summaryQ.isError &&
    summaryQ.error instanceof ApiError &&
    summaryQ.error.status === 401
  ) {
    navigate("/platform");
    return null;
  }

  const s = summaryQ.data?.data;
  const rows = listQ.data?.data ?? [];
  const meta = listQ.data?.meta;

  const tile = (
    label: string,
    value: number | undefined,
    filter?: () => void,
    tone = "text-slate-900",
  ) => (
    <button
      onClick={filter}
      disabled={!filter}
      className="text-left bg-white border border-slate-200 rounded-xl px-4 py-3 enabled:hover:border-brand-400"
    >
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`text-xl font-bold tabular-nums ${tone}`}>
        {value?.toLocaleString("en-IN") ?? "–"}
      </div>
    </button>
  );

  return (
    <PlatformShell>
      <div className="max-w-6xl mx-auto px-6 py-8 space-y-6">
        <div>
          <h1 className="text-lg font-bold text-slate-900">
            Medicine information
          </h1>
          <p className="text-sm text-slate-500">
            Descriptions and safety information for every catalogue product,
            sourced from US FDA labels for each active ingredient. Review,
            correct, and verify here.
          </p>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
          {tile("Products", s?.total, () => setStatus(""))}
          {tile(
            "Complete",
            s?.byStatus.completed,
            () => setStatus("completed"),
            "text-green-700",
          )}
          {tile(
            "Partial",
            s?.byStatus.partially_completed,
            () => setStatus("partially_completed"),
            "text-amber-700",
          )}
          {tile(
            "Needs review",
            s?.byStatus.needs_manual_review,
            () => setStatus("needs_manual_review"),
            "text-red-700",
          )}
          {tile("Verified by staff", s?.humanVerified)}
          {tile("Verified photos", s?.verifiedImages, () =>
            setImage("verified"),
          )}
          {tile("Photos awaiting review", s?.pendingImages, () =>
            setImage("needs_review"),
          )}
          {tile(
            "Partially verified",
            s?.byVerification?.PARTIALLY_VERIFIED,
            () => setVerification("PARTIALLY_VERIFIED"),
            "text-amber-700",
          )}
          {tile(
            "Flagged for review",
            s?.byVerification?.NEEDS_REVIEW,
            () => setVerification("NEEDS_REVIEW"),
            "text-red-700",
          )}
        </div>

        <div className="flex flex-wrap gap-2 items-center">
          <div className="relative flex-1 min-w-[220px]">
            <Search
              size={15}
              className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400"
            />
            <input
              id="med-search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search name or ingredient"
              className="w-full rounded-lg border border-slate-300 pl-9 pr-3 py-2 text-sm"
            />
          </div>
          <select
            id="med-status"
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPage(1);
            }}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
          >
            <option value="">All statuses</option>
            {Object.entries(STATUS_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
          <select
            id="med-image"
            value={image}
            onChange={(e) => {
              setImage(e.target.value);
              setPage(1);
            }}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
          >
            <option value="">All images</option>
            <option value="placeholder">Illustration only</option>
            <option value="needs_review">Photo awaiting review</option>
            <option value="verified">Verified photo</option>
          </select>
          <select
            id="med-research"
            value={research}
            onChange={(e) => {
              setResearch(e.target.value);
              setPage(1);
              setSelected(new Set());
            }}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
          >
            <option value="">All image research states</option>
            {Object.entries(RESEARCH_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
          <select
            id="med-verification"
            value={verification}
            onChange={(e) => {
              setVerification(e.target.value);
              setPage(1);
            }}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
          >
            <option value="">All verification states</option>
            {Object.entries(VERIFY_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>

        {selected.size > 0 && (
          <div className="flex flex-wrap items-center gap-3 rounded-xl border border-green-200 bg-green-50 px-4 py-2 text-sm">
            <span>{selected.size} selected</span>
            <button
              disabled={bulk.isPending}
              onClick={() => bulk.mutate([...selected])}
              className="rounded-lg bg-green-600 px-3 py-1.5 text-xs font-semibold text-white disabled:opacity-50"
            >
              Approve selected (only single QA-passed images are approved)
            </button>
            <button onClick={() => setSelected(new Set())} className="text-xs underline">
              Clear
            </button>
          </div>
        )}
        {bulkResult && (
          <div className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-xs text-slate-700">
            <p className="font-semibold">
              Approved {bulkResult.approved.length}; skipped {Object.keys(bulkResult.skipped).length}
            </p>
            <ul className="mt-1 list-disc pl-4">
              {Object.entries(bulkResult.skipped).map(([id, why]) => (
                <li key={id}>
                  {rows.find((r) => r.productId === id)?.name ?? id}: {why}
                </li>
              ))}
            </ul>
          </div>
        )}
        <div className="bg-white border border-slate-200 rounded-2xl overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="px-3 py-2.5 w-8">
                  <input
                    type="checkbox"
                    aria-label="Select all with a photo awaiting review"
                    checked={rows.length > 0 && rows.every((r) => !r.imageId || selected.has(r.productId))}
                    onChange={(e) =>
                      setSelected(
                        new Set(
                          e.target.checked
                            ? rows.filter((r) => r.imageVerification === "PENDING").map((r) => r.productId)
                            : [],
                        ),
                      )
                    }
                  />
                </th>
                <th className="text-left px-4 py-2.5 w-14"></th>
                <th className="text-left px-4 py-2.5">Product</th>
                <th className="text-left px-4 py-2.5">Verification</th>
                <th className="text-left px-4 py-2.5">Status</th>
                <th className="text-left px-4 py-2.5">Image</th>
                <th className="text-left px-4 py-2.5">Missing</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {rows.map((r) => (
                <tr
                  key={r.productId}
                  onClick={() => setEditing(r)}
                  className="cursor-pointer hover:bg-slate-50"
                >
                  <td className="px-3 py-2" onClick={(e) => e.stopPropagation()}>
                    {r.imageVerification === "PENDING" && (
                      <input
                        type="checkbox"
                        aria-label={`Select ${r.name}`}
                        checked={selected.has(r.productId)}
                        onChange={(e) =>
                          setSelected((s) => {
                            const n = new Set(s);
                            if (e.target.checked) n.add(r.productId);
                            else n.delete(r.productId);
                            return n;
                          })
                        }
                      />
                    )}
                  </td>
                  <td className="px-4 py-2">
                    {r.imageUrl ? (
                      <img
                        src={r.imageUrl}
                        alt=""
                        className="h-10 w-10 rounded-lg object-contain bg-slate-50"
                        loading="lazy"
                      />
                    ) : (
                      <ImageOff size={18} className="text-slate-300" />
                    )}
                  </td>
                  <td className="px-4 py-2">
                    <div className="font-medium text-slate-800 flex items-center gap-1.5">
                      {r.name}
                      {r.lastVerifiedAt && (
                        <CheckCircle2
                          size={14}
                          className="text-green-600"
                          aria-label="Verified by staff"
                        />
                      )}
                    </div>
                    <div className="text-xs text-slate-500">
                      {r.genericName}
                    </div>
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={`text-xs font-medium px-2 py-0.5 rounded-full whitespace-nowrap ${VERIFY_STYLE[r.verificationStatus]}`}
                      title={r.reviewReasons.join("\n")}
                    >
                      {VERIFY_LABEL[r.verificationStatus]}
                    </span>
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={`text-xs font-medium px-2 py-0.5 rounded-full ${STATUS_STYLE[r.status]}`}
                    >
                      {STATUS_LABEL[r.status]}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-xs text-slate-500">
                    {IMAGE_LABEL[r.imageStatus]}
                  </td>
                  <td className="px-4 py-2 text-xs text-slate-500">
                    {[
                      ...r.unmatchedIngredients.map((u) => `no source: ${u}`),
                      ...r.missingFields
                        .filter((f) => f !== "verified_image" && f !== "manufacturer")
                        .map((f) => FIELD_LABEL[f] ?? f.replace(/_/g, " ")),
                    ].join(", ") || "—"}
                  </td>
                </tr>
              ))}
              {listQ.isSuccess && rows.length === 0 && (
                <tr>
                  <td
                    colSpan={7}
                    className="px-4 py-10 text-center text-slate-400"
                  >
                    No products match these filters.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {meta && meta.totalPages > 1 && (
          <div className="flex items-center justify-between text-sm text-slate-500">
            <span>
              Page {meta.page} of {meta.totalPages} ·{" "}
              {meta.totalItems.toLocaleString("en-IN")} products
            </span>
            <div className="flex gap-2">
              <button
                disabled={page <= 1}
                onClick={() => setPage((p) => p - 1)}
                className="px-3 py-1.5 rounded-lg border border-slate-300 disabled:opacity-40"
              >
                Previous
              </button>
              <button
                disabled={page >= meta.totalPages}
                onClick={() => setPage((p) => p + 1)}
                className="px-3 py-1.5 rounded-lg border border-slate-300 disabled:opacity-40"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>

      {editing && (
        <EditPanel
          key={editing.productId}
          item={editing}
          onClose={() => setEditing(null)}
          onSaved={(updated) => {
            setEditing(updated);
            void qc.invalidateQueries({ queryKey: ["enrichment-list"] });
            void qc.invalidateQueries({ queryKey: ["enrichment-summary"] });
          }}
        />
      )}
    </PlatformShell>
  );
}

function EditPanel({
  item,
  onClose,
  onSaved,
}: {
  item: MedicineInfo;
  onClose: () => void;
  onSaved: (m: MedicineInfo) => void;
}) {
  const [form, setForm] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      FIELDS.map((f) => [f.key, (item[f.key] as string | null) ?? ""]),
    ),
  );
  const [imageUrl, setImageUrl] = useState("");
  const [imageSource, setImageSource] = useState("");
  const [imageNote, setImageNote] = useState("");

  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) =>
      api.patch<Envelope<MedicineInfo>>(
        `/api/v1/platform/enrichment/products/${item.productId}`,
        body,
      ),
    onSuccess: (res) => onSaved(res.data),
  });

  const changed = () =>
    Object.fromEntries(
      FIELDS.filter(
        (f) => form[f.key] !== ((item[f.key] as string | null) ?? ""),
      ).map((f) => [f.key, form[f.key]]),
    );

  return (
    <div
      className="fixed inset-0 z-40 flex justify-end bg-slate-900/30"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl h-full overflow-y-auto bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sticky top-0 bg-white border-b border-slate-200 px-5 py-4 flex items-start justify-between gap-3">
          <div>
            <h2 className="font-bold text-slate-900">{item.name}</h2>
            <p className="text-xs text-slate-500">
              {VERIFY_LABEL[item.verificationStatus]} ·{" "}
              {STATUS_LABEL[item.status]}
              {item.lastVerifiedAt
                ? ` · verified ${new Date(item.lastVerifiedAt).toLocaleDateString("en-IN")}`
                : " · not yet verified by staff"}
            </p>
          </div>
          <button
            onClick={onClose}
            aria-label="Close"
            className="text-slate-400 hover:text-slate-700"
          >
            <X size={18} />
          </button>
        </div>

        <div className="px-5 py-4 space-y-5">
          <div className="flex gap-4 items-start">
            <div className="h-28 w-28 shrink-0 rounded-xl bg-slate-50 border border-slate-200 flex items-center justify-center overflow-hidden">
              {item.imageUrl ? (
                <img
                  src={item.imageUrl}
                  alt=""
                  className="h-full w-full object-contain"
                />
              ) : (
                <ImageOff className="text-slate-300" />
              )}
            </div>
            <div className="flex-1 space-y-2 text-sm">
              <p className="text-xs text-slate-500">
                {IMAGE_LABEL[item.imageStatus]}
                {item.imageSource ? ` · ${item.imageSource}` : ""}
                {" · "}
                {RESEARCH_LABEL[item.imageResearchStatus]}
              </p>
              {item.imageVerification === "PENDING" && (
                <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs text-slate-600">
                  <dt className="text-slate-400">Resolution</dt>
                  <dd>
                    {item.imageWidth && item.imageHeight ? `${item.imageWidth} × ${item.imageHeight}` : "unknown"}
                    {item.imageFormat ? ` · ${item.imageFormat.toUpperCase()} original` : ""}
                  </dd>
                  <dt className="text-slate-400">QA</dt>
                  <dd className={item.imageQaStatus === "QA_PASSED" ? "text-green-700" : "text-amber-700"}>
                    {item.imageQaStatus ?? "not run"}
                  </dd>
                  <dt className="text-slate-400">Source type</dt>
                  <dd>{item.imageSourceType?.replace(/_/g, " ") ?? "—"}</dd>
                  {item.imageQaNotes && (
                    <>
                      <dt className="text-slate-400">QA notes</dt>
                      <dd className="text-amber-800">{item.imageQaNotes}</dd>
                    </>
                  )}
                </dl>
              )}
              {item.imageVerification === "PENDING" && (
                <div className="text-xs bg-amber-50 border border-amber-200 text-amber-900 rounded-lg px-3 py-2 space-y-1">
                  <p className="font-semibold">
                    Hidden from customers until approved. Compare the pack with
                    the product name: brand, strength, form and pack size.
                  </p>
                  {item.imagePackText && (
                    <p>Researcher read on the pack: “{item.imagePackText}”</p>
                  )}
                  <p className="space-x-3">
                    {item.imageSourcePageUrl && (
                      <a href={item.imageSourcePageUrl} target="_blank" rel="noreferrer" className="underline">
                        Source page
                      </a>
                    )}
                    {item.imageSourceUrl && (
                      <a href={item.imageSourceUrl} target="_blank" rel="noreferrer" className="underline">
                        Original image URL
                      </a>
                    )}
                    {item.imageOriginalUrl && (
                      <a href={item.imageOriginalUrl} target="_blank" rel="noreferrer" className="underline">
                        Stored original file
                      </a>
                    )}
                  </p>
                </div>
              )}
              <input
                id="img-url"
                value={imageUrl}
                onChange={(e) => setImageUrl(e.target.value)}
                placeholder="Replace with image URL (pack shot you have rights to)"
                className="w-full rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
              />
              <input
                id="img-src"
                value={imageSource}
                onChange={(e) => setImageSource(e.target.value)}
                placeholder="Image source, e.g. manufacturer name"
                className="w-full rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
              />
              <div className="flex gap-2">
                <button
                  disabled={!imageUrl.trim() || save.isPending}
                  onClick={() => save.mutate({ imageUrl, imageSource })}
                  className="text-xs font-semibold px-3 py-1.5 rounded-lg border border-slate-300 disabled:opacity-40"
                >
                  Replace image
                </button>
              </div>
              {item.imageVerification === "PENDING" && (
                <div className="flex flex-wrap gap-2">
                  {DECISIONS.map((d) => (
                    <button
                      key={d.key}
                      disabled={save.isPending}
                      onClick={() => save.mutate({ imageDecision: d.key, imageId: item.imageId, imageNote })}
                      className={`text-xs font-semibold px-3 py-1.5 rounded-lg disabled:opacity-50 ${d.style}`}
                    >
                      {d.label}
                    </button>
                  ))}
                </div>
              )}
              {item.imageVerification === "PENDING" && (
                <input
                  id="img-note"
                  value={imageNote}
                  onChange={(e) => setImageNote(e.target.value)}
                  placeholder="Note (e.g. why rejected)"
                  className="w-full rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
                />
              )}
            </div>
          </div>

          {item.infoSourceUrl && (
            <p className="text-xs text-slate-500">
              Source: {item.infoSource} ·{" "}
              <a
                href={item.infoSourceUrl}
                target="_blank"
                rel="noreferrer"
                className="underline"
              >
                open label
              </a>
            </p>
          )}
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 rounded-lg border border-slate-200 px-3 py-2 text-xs">
            <dt className="text-slate-400">Composition</dt>
            <dd>{item.genericName ?? "—"}</dd>
            <dt className="text-slate-400">Strength · form</dt>
            <dd>
              {item.strength ?? "—"} · {item.dosageForm?.toLowerCase() ?? "—"}
            </dd>
            <dt className="text-slate-400">Marketer</dt>
            <dd>{item.marketer ?? "not confirmed"}</dd>
            <dt className="text-slate-400">Manufacturer</dt>
            <dd>
              {item.manufacturerName ?? "not named on pack or official document"}
              {item.manufacturerEvidence && <span className="block text-slate-400">{item.manufacturerEvidence}</span>}
            </dd>
            {item.catalogueCleanup && (
              <>
                <dt className="text-slate-400">Catalogue</dt>
                <dd className="text-red-700">{item.catalogueCleanup}</dd>
              </>
            )}
          </dl>
          <CandidateCompare productId={item.productId} />
          {item.reviewReasons.length > 0 && (
            <div className="text-xs bg-red-50 border border-red-200 text-red-900 rounded-lg px-3 py-2">
              <p className="font-semibold mb-1">Why this needs review</p>
              <ul className="list-disc pl-4 space-y-0.5">
                {item.reviewReasons.map((r) => (
                  <li key={r}>{r}</li>
                ))}
              </ul>
            </div>
          )}
          {(item.manufacturerSourceUrl || item.officialProductUrl) && (
            <p className="text-xs text-slate-500 space-x-2">
              {item.manufacturerSourceUrl && (
                <a href={item.manufacturerSourceUrl} target="_blank" rel="noreferrer" className="underline">
                  Manufacturer source
                </a>
              )}
              {item.officialProductUrl && (
                <a href={item.officialProductUrl} target="_blank" rel="noreferrer" className="underline">
                  Official product page
                </a>
              )}
            </p>
          )}
          {item.unmatchedIngredients.length > 0 && (
            <p className="text-xs bg-amber-50 border border-amber-200 text-amber-900 rounded-lg px-3 py-2">
              No authoritative source found for:{" "}
              {item.unmatchedIngredients.join(", ")}. Fill these in from the
              manufacturer's leaflet.
            </p>
          )}

          {FIELDS.map((f) => (
            <label key={f.key} className="block">
              <span className="block text-xs font-medium text-slate-600 mb-1">
                {f.label}
              </span>
              {f.rows === 1 ? (
                <input
                  id={`f-${f.key}`}
                  value={form[f.key]}
                  onChange={(e) =>
                    setForm((s) => ({ ...s, [f.key]: e.target.value }))
                  }
                  className="w-full rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
                />
              ) : (
                <textarea
                  id={`f-${f.key}`}
                  rows={f.rows}
                  value={form[f.key]}
                  onChange={(e) =>
                    setForm((s) => ({ ...s, [f.key]: e.target.value }))
                  }
                  className="w-full rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
                />
              )}
            </label>
          ))}

          {save.isError && (
            <p className="text-sm text-red-600">
              {save.error instanceof ApiError
                ? save.error.message
                : "Could not save."}
            </p>
          )}
        </div>

        <div className="sticky bottom-0 bg-white border-t border-slate-200 px-5 py-3 flex flex-wrap gap-2 justify-end">
          <button
            disabled={save.isPending}
            onClick={() => save.mutate(changed())}
            className="text-sm font-semibold px-4 py-2 rounded-lg border border-slate-300"
          >
            Save changes
          </button>
          <button
            disabled={save.isPending}
            onClick={() =>
              save.mutate({ ...changed(), verified: !item.lastVerifiedAt })
            }
            className="text-sm font-semibold px-4 py-2 rounded-lg bg-brand-600 text-white"
          >
            {item.lastVerifiedAt
              ? "Remove verification"
              : "Save and mark verified"}
          </button>
        </div>
      </div>
    </div>
  );
}

// Side-by-side view of every stored candidate for one product, so a reviewer
// can compare when more than one image exists. Rejected ones stay visible
// (greyed) with the reason, so the same wrong image is not re-added.
function CandidateCompare({ productId }: { productId: string }) {
  const q = useQuery({
    queryKey: ["enrichment-images", productId],
    queryFn: () =>
      api.get<Envelope<ImageCandidate[]>>(`/api/v1/platform/enrichment/products/${productId}/images`),
  });
  const list = q.data?.data ?? [];
  if (list.length < 2) return null;
  return (
    <div>
      <p className="mb-1 text-xs font-semibold text-slate-600">All image candidates ({list.length})</p>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {list.map((c) => (
          <figure
            key={c.id}
            className={`rounded-lg border p-1.5 text-[11px] ${c.verificationStatus === "REJECTED" ? "opacity-50" : ""}`}
          >
            <img src={c.url} alt="" loading="lazy" className="h-24 w-full object-contain" />
            <figcaption className="mt-1 space-y-0.5 text-slate-600">
              <span className="block font-semibold">{c.verificationStatus}{c.reviewAction ? ` · ${c.reviewAction}` : ""}</span>
              <span className="block">{c.width && c.height ? `${c.width}×${c.height}` : ""} {c.sourceName ?? ""}</span>
              {c.qaNotes && <span className="block text-amber-700">{c.qaNotes}</span>}
            </figcaption>
          </figure>
        ))}
      </div>
    </div>
  );
}
