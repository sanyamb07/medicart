import { useEffect } from "react";
import { Link, NavLink, useParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";

// Help centre and short health guides. These pages previously 404'd from the
// footer. Content describes how this storefront works (from the order and
// prescription flows in the API) plus general, non-diagnostic guidance.

interface Topic {
  title: string;
  group: "Help" | "Guides";
  intro: string;
  sections: { h: string; p: string[] }[];
}

const TOPICS: Record<string, Topic> = {
  delivery: {
    title: "Delivery",
    group: "Help",
    intro: "Delivery time, fee and cash-on-delivery availability depend on your PIN code. You'll always see them before you place an order.",
    sections: [
      { h: "Checking your area", p: ["Set your PIN code from the location selector at the top of any page, or enter it at checkout. We show the delivery window, the delivery fee and whether cash on delivery is available."] },
      { h: "When the clock starts", p: ["Delivery windows count from when your order is confirmed: after you confirm it on WhatsApp and, for prescription items, after a pharmacist has reviewed your prescription."] },
      { h: "Tracking", p: ["Use your order reference on the Track an order page, or open Orders in your account. Status updates are also sent to the WhatsApp number on the order."] },
    ],
  },
  returns: {
    title: "Returns & cancellations",
    group: "Help",
    intro: "You can cancel an order yourself any time before you pay. After payment, cancellations and refunds are handled by our pharmacy team.",
    sections: [
      { h: "Cancelling before payment", p: ["Until payment is complete, you can cancel from the Track an order page using your order reference and mobile number. No charge is made."] },
      { h: "After payment", p: ["Message us on the WhatsApp thread for your order. If the order can still be stopped, we'll cancel it and start a refund."] },
      { h: "Damaged, wrong or expired items", p: ["For safety reasons medicines generally can't be returned once delivered. If something arrives damaged, incorrect or expired, contact us on your order's WhatsApp thread with a photo of the item and its batch label."] },
      { h: "Refunds", p: ["Refunds go back to the original payment method. Your order status will show 'Refund in progress' and then 'Refunded'."] },
    ],
  },
  prescriptions: {
    title: "Prescriptions",
    group: "Help",
    intro: "Medicines marked Rx can only be dispensed against a valid prescription from a registered medical practitioner. Every one is checked by a pharmacist.",
    sections: [
      { h: "What makes a prescription valid", p: ["It should show the doctor's name and registration number, the patient's name, the date, the medicine name with strength and dosage, and the doctor's signature or clinic stamp.", "Make sure the whole page is visible and readable in your photo or scan."] },
      { h: "How review works", p: ["Upload your prescription from your account or the Upload Rx page. A pharmacist reviews it, and orders containing Rx items are held until it's verified. You're only asked to pay after that review is complete."] },
      { h: "Privacy", p: ["Uploads are stored privately and are visible only to you and our pharmacist reviewers. Access is checked on every request."] },
    ],
  },
  payments: {
    title: "Payments",
    group: "Help",
    intro: "Nothing is charged when you place an order. You pay once the order is confirmed.",
    sections: [
      { h: "How payment works", p: ["After you place an order we open WhatsApp with your order details so you can confirm it. Once confirmed (and, for Rx items, once your prescription is approved) you'll get a secure link to pay by UPI, card or net banking."] },
      { h: "Cash on delivery", p: ["Available in some areas. Your PIN code check shows whether it's offered for your address."] },
      { h: "This demo", p: ["MediCart is a demonstration environment. Payments run in the provider's test mode and no real money moves."] },
    ],
  },
  generics: {
    title: "Branded vs generic medicines",
    group: "Guides",
    intro: "The 'salt' is the active ingredient in a medicine. Different companies can sell the same salt, at the same strength, under different brand names and at different prices.",
    sections: [
      { h: "Why prices differ", p: ["Brand building, packaging and distribution all affect price. Medicines with the same salt, strength and dosage form are intended to have the same effect, but they are not always interchangeable for every person."] },
      { h: "Comparing on MediCart", p: ["Search by salt name (for example 'atorvastatin') to see every brand that contains it, then sort by price. Each product page also lists other brands with the same composition."] },
      { h: "Before you switch", p: ["Check that the strength and form (tablet, syrup, extended-release and so on) match what you were prescribed, and ask your doctor or pharmacist before changing brands, especially for long-term treatment."] },
    ],
  },
  storage: {
    title: "Storing medicines at home",
    group: "Guides",
    intro: "Most medicines keep best somewhere cool, dry and dark. The label on each product tells you exactly what it needs.",
    sections: [
      { h: "Avoid heat and humidity", p: ["Bathrooms and kitchens get warm and damp, which can degrade tablets and capsules. A bedroom cupboard is usually better.", "Don't leave medicines in a parked car."] },
      { h: "Follow the label", p: ["Some products, such as certain insulins, eye drops and vaccines, need refrigeration. Others must not be frozen. Keep medicines in their original packaging so the batch and expiry stay with them."] },
      { h: "Expired or unused medicines", p: ["Check expiry dates regularly. Don't use medicines past expiry, and ask your pharmacist about safe disposal rather than throwing them in household waste."] },
      { h: "Keep them safe", p: ["Store all medicines out of the reach and sight of children and pets."] },
    ],
  },
};

export default function Help() {
  const { topic = "delivery" } = useParams();
  const t = TOPICS[topic];

  useEffect(() => {
    document.title = `${t?.title ?? "Help"} | MediCart`;
  }, [t]);

  if (!t) {
    return (
      <div className="page max-w-xl py-20 text-center">
        <h1 className="text-2xl font-bold text-ink-900">Help topic not found</h1>
        <Link to="/help/delivery" className="btn btn-primary mt-6">
          Go to help centre
        </Link>
      </div>
    );
  }

  return (
    <div className="page py-8 sm:py-12">
      <div className="grid gap-8 lg:grid-cols-[14rem_1fr] lg:gap-14">
        <nav aria-label="Help topics" className="lg:sticky lg:top-36 lg:self-start">
          {(["Help", "Guides"] as const).map((g) => (
            <div key={g} className="mb-6">
              <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-400">{g === "Help" ? "Help centre" : "Health guides"}</p>
              <ul className="no-scrollbar flex gap-2 overflow-x-auto lg:block lg:space-y-0.5">
                {Object.entries(TOPICS)
                  .filter(([, v]) => v.group === g)
                  .map(([slug, v]) => (
                    <li key={slug}>
                      <NavLink
                        to={`/help/${slug}`}
                        className={({ isActive }) =>
                          `block whitespace-nowrap rounded-lg px-3 py-2 text-sm font-medium ${isActive ? "bg-brand-50 text-brand-800" : "text-ink-600 hover:bg-ink-100"}`
                        }
                      >
                        {v.title}
                      </NavLink>
                    </li>
                  ))}
              </ul>
            </div>
          ))}
        </nav>

        <article className="max-w-2xl">
          <Link to="/" className="mb-4 inline-flex items-center gap-1 text-sm font-medium text-ink-500 hover:text-ink-800 lg:hidden">
            <ArrowLeft size={15} /> Home
          </Link>
          <p className="eyebrow">{t.group === "Help" ? "Help centre" : "Health guide"}</p>
          <h1 className="mt-2 text-3xl font-bold text-ink-900 sm:text-4xl">{t.title}</h1>
          <p className="mt-4 text-lg leading-relaxed text-ink-600">{t.intro}</p>
          <div className="mt-8 space-y-8">
            {t.sections.map((s) => (
              <section key={s.h}>
                <h2 className="text-xl font-bold text-ink-900">{s.h}</h2>
                {s.p.map((para) => (
                  <p key={para} className="mt-2 leading-relaxed text-ink-700">
                    {para}
                  </p>
                ))}
              </section>
            ))}
          </div>
          {t.group === "Guides" && (
            <p className="mt-10 rounded-xl bg-ink-100 p-4 text-sm text-ink-600">
              This guide is general information, not medical advice. Always follow the instructions from your doctor or pharmacist.
            </p>
          )}
        </article>
      </div>
    </div>
  );
}
