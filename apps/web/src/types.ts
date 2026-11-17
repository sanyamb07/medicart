// Shared types mirroring the Go API's JSON shapes. Kept hand-written and
// colocated in the frontend app rather than in packages/shared-types for
// this iteration — see docs/IMPLEMENTATION_PLAN.md.

export interface Product {
  id: string;
  sku: string;
  slug: string;
  name: string;
  brandName: string | null;
  brandSlug: string | null;
  manufacturer: string | null;
  genericName: string | null;
  strength: string | null;
  dosageForm: string | null;
  packSize: string | null;
  description: string | null;
  keyUses: string | null;
  storageInfo: string | null;
  manufacturerInfo: string | null;
  mrpPaise: number;
  pricePaise: number;
  discountPercent: number;
  requiresPrescription: boolean;
  maxQtyPerOrder: number;
  isDemoData: boolean;
  isFeatured: boolean;
  inStock: boolean;
  availableQty: number;
  imageUrl: string | null;
  isActive: boolean;
  ingredients?: { name: string; strength: string | null }[];
  categories?: { id: string; name: string; slug: string }[];
}

export interface Batch {
  id: string;
  productId: string;
  batchNumber: string;
  expiryDate: string;
  qtyReceived: number;
  qtyOnHand: number;
  qtyReserved: number;
  qtyAvailable: number;
  isBlocked: boolean;
  blockedReason: string | null;
  isExpired: boolean;
  receivedAt: string;
}

export interface Category {
  id: string;
  parentId: string | null;
  name: string;
  slug: string;
  icon: string | null;
  sortOrder: number;
  productCount: number;
}

export interface CartLine {
  productId: string;
  productSlug: string;
  productName: string;
  productSku: string;
  brandName: string | null;
  imageUrl: string | null;
  packSize: string | null;
  qty: number;
  unitMrpPaise: number;
  unitPricePaise: number;
  lineTotalPaise: number;
  requiresPrescription: boolean;
  availableQty: number;
  maxQtyPerOrder: number;
  exceeded: boolean;
  isActive: boolean;
}

export interface CartSummary {
  cartId: string;
  lines: CartLine[];
  subtotalPaise: number;
  itemCount: number;
  requiresPrescription: boolean;
  hasIssues: boolean;
}

export interface OrderItem {
  id: string;
  productId: string;
  productName: string;
  productSku: string;
  brandName: string | null;
  strength: string | null;
  dosageForm: string | null;
  packSize: string | null;
  qty: number;
  unitMrpPaise: number;
  unitPricePaise: number;
  lineTotalPaise: number;
  requiresPrescription: boolean;
}

export interface StatusEvent {
  fromStatus: string | null;
  toStatus: string;
  reason: string | null;
  actorType: string;
  createdAt: string;
}

export interface PaymentSummary {
  status: string;
  amountPaise: number;
  isTestMode: boolean;
  paymentUrl: string | null;
  method: string | null;
}

export interface Order {
  id: string;
  reference: string;
  status: string;
  recipientName: string;
  contactMobile: string;
  addressLine1: string;
  addressLine2: string | null;
  landmark: string | null;
  city: string;
  state: string;
  pinCode: string;
  deliveryNotes: string | null;
  subtotalPaise: number;
  discountPaise: number;
  deliveryFeePaise: number;
  taxPaise: number;
  totalPaise: number;
  requiresPrescription: boolean;
  whatsappHandoffAt: string | null;
  whatsappConfirmedAt: string | null;
  cancelledReason?: string | null;
  placedAt: string;
  items: OrderItem[];
  history?: StatusEvent[];
  payment?: PaymentSummary | null;
}

export interface OrderListItem {
  id: string;
  reference: string;
  status: string;
  recipientName: string;
  contactMobile: string;
  totalPaise: number;
  itemCount: number;
  placedAt: string;
}

export interface ServiceabilityArea {
  pinCode: string;
  city: string;
  state: string;
  isServiceable: boolean;
  etaMinDays: number | null;
  etaMaxDays: number | null;
  codAvailable: boolean;
  deliveryFeePaise: number;
}

export interface Prescription {
  id: string;
  customerId: string;
  originalName: string | null;
  contentType: string;
  sizeBytes: number;
  status: "PENDING_REVIEW" | "APPROVED" | "REJECTED" | "EXPIRED";
  doctorName: string | null;
  doctorRegNo: string | null;
  issuedOn: string | null;
  validUntil: string | null;
  uploadedAt: string;
}

export const PRESCRIPTION_STATUS_LABELS: Record<Prescription["status"], string> = {
  PENDING_REVIEW: "Awaiting pharmacist review",
  APPROVED: "Approved",
  REJECTED: "Rejected",
  EXPIRED: "Expired",
};

export const STATUS_LABELS: Record<string, string> = {
  CREATED: "Created",
  AWAITING_CUSTOMER_CONFIRMATION: "Awaiting your WhatsApp confirmation",
  AWAITING_PRESCRIPTION_REVIEW: "Prescription under review",
  AWAITING_PAYMENT: "Confirmed — awaiting payment",
  PAYMENT_PENDING: "Payment in progress",
  PAID: "Payment received",
  PROCESSING: "Being prepared",
  READY_FOR_DISPATCH: "Ready for dispatch",
  SHIPPED: "Shipped",
  DELIVERED: "Delivered",
  CANCELLED: "Cancelled",
  REFUND_PENDING: "Refund in progress",
  REFUNDED: "Refunded",
};

// ---------------------------------------------------------------- multi-tenant

export interface Business {
  id: string;
  slug: string;
  name: string;
  status: "PENDING" | "ACTIVE" | "SUSPENDED" | "REJECTED";
  logoUrl: string | null;
  tagline: string | null;
  supportWhatsapp: string | null;
  licenseStatus: "UNVERIFIED" | "SUBMITTED" | "VERIFIED";
  createdAt: string;
}

export interface BusinessCatalogRow {
  productId: string;
  sku: string;
  name: string;
  brandName: string | null;
  mrpPaise: number;
  masterPricePaise: number;
  isListed: boolean;
  pricePaise: number | null;
  isVisible: boolean;
  isFeatured: boolean;
  minOrderQty: number;
  requiresPrescription: boolean;
}

export type LeadStatus = "NEW" | "CONTACTED" | "INTERESTED" | "AWAITING_RESPONSE" | "CONVERTED" | "LOST";

export const LEAD_STATUS_LABELS: Record<LeadStatus, string> = {
  NEW: "New",
  CONTACTED: "Contacted",
  INTERESTED: "Interested",
  AWAITING_RESPONSE: "Awaiting response",
  CONVERTED: "Converted",
  LOST: "Lost",
};

export interface CrmCustomer {
  id: string;
  customerId: string;
  mobile: string | null;
  fullName: string | null;
  leadStatus: LeadStatus;
  notes: string | null;
  orderCount: number;
  lifetimeValuePaise: number;
  firstSeenAt: string;
  lastOrderAt: string | null;
}

export interface Enquiry {
  id: string;
  customerId: string | null;
  mobile: string | null;
  fullName: string | null;
  channel: string;
  productsDiscussed: string[];
  searchQueries: string[];
  intent: string | null;
  status: LeadStatus;
  contactName: string | null;
  contactMobile: string | null;
  contactEmail: string | null;
  followUpAt: string | null;
  notes: string | null;
  createdAt: string;
  updatedAt: string;
}

export type AffiliateStatus = "PENDING" | "ACTIVE" | "SUSPENDED" | "REJECTED";

export interface Affiliate {
  id: string;
  code: string;
  fullName: string;
  email: string;
  phone: string | null;
  payoutDetails?: string | null;
  status: AffiliateStatus;
  rejectedReason?: string | null;
  createdAt: string;
  approvedAt: string | null;
}

export type ReferralStatus = "PENDING" | "CONFIRMED" | "VOID" | "PAID";

export interface Referral {
  id: string;
  orderId: string;
  orderReference: string;
  codeUsed: string;
  eligibleSubtotalPaise: number;
  commissionRateBps: number;
  commissionPaise: number;
  status: ReferralStatus;
  createdAt: string;
  confirmedAt: string | null;
  paidAt: string | null;
}

export interface AdminReferral extends Referral {
  affiliateId: string;
  affiliateCode: string;
  affiliateName: string;
}

export interface AffiliateSettings {
  commissionRateBps: number;
  updatedAt: string;
}

export type EnrichmentStatus =
  | "pending" | "processing" | "partially_completed" | "completed" | "needs_manual_review" | "failed";

export interface MedicineInfo {
  productId: string;
  name: string;
  genericName: string | null;
  status: EnrichmentStatus;
  therapeuticCategory: string | null;
  shortDescription: string | null;
  description: string | null;
  uses: string | null;
  mechanismOfAction: string | null;
  precautions: string | null;
  sideEffects: string | null;
  warnings: string | null;
  contraindications: string | null;
  storageInfo: string | null;
  manufacturer: string | null;
  seoTitle: string | null;
  metaDescription: string | null;
  infoSource: string | null;
  infoSourceUrl: string | null;
  imageUrl: string | null;
  imageStatus: "placeholder" | "needs_review" | "verified";
  imageSource: string | null;
  missingFields: string[];
  unmatchedIngredients: string[];
  lastVerifiedAt: string | null;
  lastError?: string | null;
  // Provenance of the image under review (admin) or the shown image (public).
  imageVerification: "PENDING" | "VERIFIED" | "REJECTED" | null;
  imageSourceUrl: string | null;
  imageSourcePageUrl: string | null;
  imagePackText?: string | null;
  verificationStatus: VerificationStatus;
  reviewReasons: string[];
  regulatoryGroup?: string | null;
  manufacturerSourceUrl: string | null;
  officialProductUrl: string | null;
  strength: string | null;
  dosageForm: string | null;
  marketer: string | null;
  manufacturerName: string | null;
  manufacturerEvidence?: string | null;
  imageResearchStatus: ImageResearchStatus;
  imageResearchNote?: string | null;
  catalogueCleanup?: string | null;
  imageId?: string | null;
  imageWidth?: number | null;
  imageHeight?: number | null;
  imageFormat?: string | null;
  imageQaStatus?: "QA_PASSED" | "QA_FAILED" | "MANUAL_VERIFICATION_REQUIRED" | null;
  imageQaNotes?: string | null;
  imageSourceType?: string | null;
  imageOriginalUrl?: string | null;
  imageReviewAction?: string | null;
}

export type ImageResearchStatus =
  | "NOT_FOUND"
  | "RESEARCH_PENDING"
  | "IMAGE_FOUND"
  | "QA_PENDING"
  | "QA_PASSED"
  | "PHARMACIST_REVIEW_PENDING"
  | "APPROVED"
  | "REJECTED"
  | "MANUAL_VERIFICATION_REQUIRED";

export interface ImageCandidate {
  id: string;
  url: string;
  originalUrl: string | null;
  sourceName: string | null;
  sourceUrl: string | null;
  sourcePageUrl: string | null;
  sourceType: string | null;
  width: number | null;
  height: number | null;
  format: string | null;
  qaStatus: string | null;
  qaNotes: string | null;
  packText: string | null;
  verificationStatus: "PENDING" | "VERIFIED" | "REJECTED";
  reviewAction: string | null;
  notes: string | null;
  createdAt: string;
}

export type VerificationStatus = "VERIFIED" | "PARTIALLY_VERIFIED" | "NEEDS_REVIEW";
