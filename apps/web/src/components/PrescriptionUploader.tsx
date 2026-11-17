import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Clock, FileText, Loader2, UploadCloud, XCircle } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { PRESCRIPTION_STATUS_LABELS, type Prescription } from "../types";
import { useToast } from "./Toast";

const STATUS_STYLE: Record<Prescription["status"], { icon: typeof CheckCircle2; cls: string }> = {
  PENDING_REVIEW: { icon: Clock, cls: "bg-rx-50 text-rx-700" },
  APPROVED: { icon: CheckCircle2, cls: "bg-save-50 text-save-700" },
  REJECTED: { icon: XCircle, cls: "bg-red-50 text-red-700" },
  EXPIRED: { icon: XCircle, cls: "bg-ink-100 text-ink-600" },
};

const ACCEPT = "image/jpeg,image/png,image/heic,image/webp,application/pdf";

// Upload + list of the signed-in customer's prescriptions. Uses the existing
// /api/v1/me/prescriptions endpoints unchanged. Supports drag-and-drop on
// desktop and the native file/camera picker on phones.
export default function PrescriptionUploader() {
  const queryClient = useQueryClient();
  const toast = useToast();
  const fileInput = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["my-prescriptions"],
    queryFn: () => api.get<Envelope<Prescription[]>>("/api/v1/me/prescriptions"),
  });

  async function handleFile(file: File) {
    setError(null);
    if (file.size > 10 * 1024 * 1024) {
      setError("That file is larger than 10 MB. Please upload a smaller photo or PDF.");
      return;
    }
    setUploading(true);
    try {
      const form = new FormData();
      form.append("file", file);
      await api.upload("/api/v1/me/prescriptions", form);
      await queryClient.invalidateQueries({ queryKey: ["my-prescriptions"] });
      toast({ tone: "success", title: "Prescription uploaded for review" });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Upload failed. Please try again.");
    } finally {
      setUploading(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  }

  const list = data?.data ?? [];

  return (
    <div>
      <label
        onDragOver={(e) => {
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          const f = e.dataTransfer.files?.[0];
          if (f) void handleFile(f);
        }}
        className={`flex cursor-pointer flex-col items-center justify-center gap-3 rounded-2xl border-2 border-dashed px-6 py-10 text-center transition-colors focus-within:ring-2 focus-within:ring-brand-500 ${
          dragging ? "border-brand-500 bg-brand-50" : "border-ink-300 bg-white hover:border-brand-400 hover:bg-brand-50/40"
        } ${uploading ? "pointer-events-none opacity-70" : ""}`}
      >
        <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-50 text-brand-700">
          {uploading ? <Loader2 size={26} className="animate-spin" /> : <UploadCloud size={26} />}
        </span>
        <span className="text-base font-semibold text-ink-900">
          {uploading ? "Uploading…" : (
            <>
              <span className="text-brand-700 underline underline-offset-4">Choose a file</span> or take a photo
            </>
          )}
        </span>
        <span className="text-sm text-ink-500">JPG, PNG, HEIC, WEBP or PDF · up to 10 MB</span>
        <input
          ref={fileInput}
          type="file"
          accept={ACCEPT}
          className="sr-only"
          disabled={uploading}
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void handleFile(f);
          }}
        />
      </label>
      {error && (
        <p role="alert" className="mt-3 text-sm text-red-700">
          {error}
        </p>
      )}

      <h3 className="mt-8 font-sans text-base font-semibold text-ink-900">Your uploads</h3>
      {isLoading && <div className="skeleton mt-3 h-14 rounded-xl" />}
      {!isLoading && list.length === 0 && <p className="mt-2 text-sm text-ink-500">No prescriptions uploaded yet.</p>}
      <ul className="mt-3 space-y-2">
        {list.map((rx) => {
          const { icon: Icon, cls } = STATUS_STYLE[rx.status];
          return (
            <li key={rx.id} className="flex items-center gap-3 rounded-xl border border-ink-200/80 bg-white p-3.5">
              <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-ink-100 text-ink-500">
                <FileText size={18} />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-ink-900">{rx.originalName ?? "Prescription"}</p>
                <p className="text-xs text-ink-500">
                  Uploaded {new Date(rx.uploadedAt).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" })}
                  {rx.validUntil && ` · valid until ${new Date(rx.validUntil).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" })}`}
                </p>
              </div>
              <span className={`inline-flex shrink-0 items-center gap-1 rounded-full px-2.5 py-1 text-xs font-semibold ${cls}`}>
                <Icon size={13} /> {PRESCRIPTION_STATUS_LABELS[rx.status]}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
