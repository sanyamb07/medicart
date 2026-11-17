import { useEffect } from "react";
import { Link } from "react-router-dom";
import { CheckCircle2, FileUp, Lock, ShieldCheck } from "lucide-react";
import { useAuth } from "../hooks/useAuth";
import PrescriptionUploader from "../components/PrescriptionUploader";

export default function UploadPrescription() {
  const { user, isLoading } = useAuth();
  useEffect(() => {
    document.title = "Upload prescription | MediCart";
  }, []);

  return (
    <div className="page max-w-5xl py-8 sm:py-12">
      <div className="grid gap-8 lg:grid-cols-[1fr_20rem] lg:gap-12">
        <div>
          <p className="eyebrow">Prescription medicines</p>
          <h1 className="mt-2 text-3xl font-bold text-ink-900">Upload your prescription</h1>
          <p className="mt-2 max-w-xl text-ink-600">
            A pharmacist reviews it before any prescription medicine is dispatched. Uploading doesn't place an order. Add items to your cart as
            usual.
          </p>

          <div className="mt-8">
            {isLoading ? (
              <div className="skeleton h-48 rounded-2xl" />
            ) : user ? (
              <PrescriptionUploader />
            ) : (
              <div className="rounded-2xl border border-ink-200/80 bg-white p-8 text-center">
                <span className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-50 text-brand-700">
                  <FileUp size={26} />
                </span>
                <h2 className="mt-4 text-xl font-bold text-ink-900">Sign in to upload</h2>
                <p className="mx-auto mt-2 max-w-sm text-ink-600">
                  Prescriptions are saved to your account so only you and our pharmacist reviewers can see them, and you can reuse them on future
                  orders.
                </p>
                <Link to="/login" state={{ from: "/upload-prescription" }} className="btn btn-primary btn-lg mt-6">
                  Sign in with mobile number
                </Link>
              </div>
            )}
          </div>
        </div>

        <aside className="space-y-4">
          <div className="rounded-2xl border border-ink-200/80 bg-white p-5">
            <h2 className="font-sans font-semibold text-ink-900">A valid prescription shows</h2>
            <ul className="mt-3 space-y-2 text-sm text-ink-700">
              {["Doctor's name and registration number", "Patient's name and date", "Medicine name, strength and dosage", "Doctor's signature or clinic stamp"].map((t) => (
                <li key={t} className="flex gap-2">
                  <CheckCircle2 size={16} className="mt-0.5 shrink-0 text-brand-600" /> {t}
                </li>
              ))}
            </ul>
            <Link to="/help/prescriptions" className="mt-4 inline-block text-sm font-semibold text-brand-700 hover:underline">
              Learn more
            </Link>
          </div>
          <div className="rounded-2xl bg-ink-100/70 p-5 text-sm text-ink-700">
            <p className="flex items-center gap-2 font-semibold text-ink-900">
              <Lock size={16} /> Private and secure
            </p>
            <p className="mt-1.5">Only you and our pharmacist reviewers can open your uploads. Access is checked on every request.</p>
            <p className="mt-3 flex items-center gap-2 font-semibold text-ink-900">
              <ShieldCheck size={16} /> Reviewed by a pharmacist
            </p>
            <p className="mt-1.5">You'll see the review status here and on any order that needs it.</p>
          </div>
        </aside>
      </div>
    </div>
  );
}
