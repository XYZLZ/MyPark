/** Uppercases and strips whitespace and dashes: `a-1 23` becomes `A123`. */
export function normalizePlate(input: string): string {
  return input.replace(/[\s-]/g, '').toUpperCase();
}

/** A plate is valid when it has 2 to 10 letters or digits once normalized. */
export function isValidPlate(input: string): boolean {
  return /^[A-Z0-9]{2,10}$/.test(normalizePlate(input));
}

/** Strips whitespace and dashes. A bare `+1` (the form pre-fill) means no phone. */
export function normalizePhone(input: string): string {
  const stripped = input.replace(/[\s-]/g, '');
  return stripped === '+1' ? '' : stripped;
}
