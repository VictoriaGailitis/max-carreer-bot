import directory from '../../backend/internal/geography/cities.json' with { type: 'json' };
export type City = (typeof directory.cities)[number];
export const cities: readonly City[] = directory.cities;
export function cityKey(value: string): string {
  return value.trim().toLocaleLowerCase('ru-RU').replace(/^г\./u, '').replace(/ё/gu, 'е').replace(/[\s\-–—‑]+/gu, '');
}
const canonical = new Map(cities.flatMap(city => [city.value, ...city.aliases].map(value => [cityKey(value), city.value] as const)));
export function canonicalCity(value: string): string | undefined { return canonical.get(cityKey(value)); }
// Damerau-Levenshtein catches a swapped pair as well as a missing/extra letter.
function distance(a: string, b: string): number {
  const rows = Array.from({ length: a.length + 1 }, () => Array<number>(b.length + 1).fill(0));
  for (let i = 0; i <= a.length; i++) rows[i][0] = i;
  for (let j = 0; j <= b.length; j++) rows[0][j] = j;
  for (let i = 1; i <= a.length; i++) for (let j = 1; j <= b.length; j++) {
    rows[i][j] = Math.min(rows[i - 1][j] + 1, rows[i][j - 1] + 1, rows[i - 1][j - 1] + Number(a[i - 1] !== b[j - 1]));
    if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) rows[i][j] = Math.min(rows[i][j], rows[i - 2][j - 2] + 1);
  }
  return rows[a.length][b.length];
}
export function suggestCities(query: string, excluded: readonly string[] = []): { city: City; approximate: boolean }[] {
  const key = cityKey(query);
  if (key.length > 80) return [];
  return cities.filter(city => !excluded.includes(city.value)).map(city => {
    const names = [city.name, city.value, ...city.aliases].map(cityKey);
    const direct = !key ? 3 : names.some(name => name === key) ? 0 : names.some(name => name.startsWith(key)) ? 1 : names.some(name => name.includes(key)) ? 2 : null;
    if (direct !== null) return { city, rank: direct, approximate: false };
    if (key.length < 4) return null;
    const limit = key.length >= 7 ? 2 : 1;
    const typoDistance = Math.min(...names.filter(name => Math.abs(name.length - key.length) <= limit).map(name => distance(key, name)));
    return typoDistance <= limit ? { city, rank: 4 + typoDistance, approximate: true } : null;
  }).filter(value => value !== null).sort((a, b) => a.rank - b.rank || b.city.population - a.city.population).slice(0, 8);
}
export function cityValidation(values: readonly string[] | undefined): string | null {
  if (!values?.length) return 'Выбери город из подсказок или «Любой город».';
  if (values.length > 20) return 'Можно выбрать не больше 20 городов.';
  if (values.some(value => !canonicalCity(value))) return 'Есть город, которого нет в справочнике. Удали его и выбери название из подсказок.';
  const normalized = values.map(value => canonicalCity(value));
  if (new Set(normalized).size !== values.length) return 'Этот город уже выбран.';
  return null;
}
