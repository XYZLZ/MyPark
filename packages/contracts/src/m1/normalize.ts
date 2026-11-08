const plateSeparators = /[ -]/g;

/** Mirrors the server: strips spaces and dashes, then uppercases. `a-1 23` becomes `A123`. */
export function normalizePlate(input: string): string {
  return input.replace(plateSeparators, '').toUpperCase();
}

/**
 * Valid when 2 to 10 ASCII letters or digits remain after stripping spaces and
 * dashes. The ASCII check runs before uppercasing, so look-alikes such as the
 * dotless i or long s never map onto a Latin letter (the server rejects them too).
 */
export function isValidPlate(input: string): boolean {
  return /^[A-Za-z0-9]{2,10}$/.test(input.replace(plateSeparators, ''));
}

/** Strips whitespace and dashes. A bare `+1` (the form pre-fill) means no phone. */
export function normalizePhone(input: string): string {
  const stripped = input.replace(/[\s-]/g, '');
  return stripped === '+1' ? '' : stripped;
}
