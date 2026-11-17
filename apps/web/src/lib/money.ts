// Formats paise (integer minor units) for display. The server is always the
// source of truth for amounts; this file only renders what it returns.
export function formatPaise(paise: number): string {
  const rupees = Math.abs(paise) / 100;
  const sign = paise < 0 ? "-" : "";
  const [whole, frac] = rupees.toFixed(2).split(".");
  const grouped = groupIndian(whole);
  return `${sign}₹${grouped}.${frac}`;
}

function groupIndian(whole: string): string {
  if (whole.length <= 3) return whole;
  const last3 = whole.slice(-3);
  let rest = whole.slice(0, -3);
  const groups: string[] = [];
  while (rest.length > 2) {
    groups.unshift(rest.slice(-2));
    rest = rest.slice(0, -2);
  }
  if (rest) groups.unshift(rest);
  return `${groups.join(",")},${last3}`;
}
