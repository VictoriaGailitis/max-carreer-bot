import assert from 'node:assert/strict';
import test from 'node:test';
import { canonicalCity, cities, cityValidation, suggestCities } from '../src/cities.ts';
import { validation, cleanDraft } from '../src/model.ts';
import type { Questionnaire } from '../../api/client/client.ts';

test('typos are suggestions, not valid saved values', () => {
  assert.equal(suggestCities('Мосвка')[0]?.city.value, 'Москва');
  assert.equal(suggestCities('Мосвка')[0]?.approximate, true);
  assert.equal(canonicalCity('Мосвка'), undefined);
  assert.ok(cityValidation(['Мосвка']));
  assert.equal(cityValidation(['Москва']), null);
});
test('case, е/ё, hyphens and common abbreviations resolve to one spelling', () => {
  assert.equal(canonicalCity(' москва '), 'Москва');
  assert.equal(canonicalCity('СПБ'), 'Санкт-Петербург');
  assert.equal(canonicalCity('Санкт Петербург'), 'Санкт-Петербург');
  assert.equal(canonicalCity('Орёл'), canonicalCity('Орел'));
  assert.ok(cityValidation(['Москва', 'МОСКВА']));
});
test('same-named cities require region and nonexistent cities do not pass', () => {
  assert.equal(canonicalCity('Берёзовский'), undefined);
  const options = suggestCities('Березовский');
  assert.ok(options.filter(option => option.city.name === 'Березовский' || option.city.name === 'Берёзовский').length >= 2);
  for (const option of options) assert.ok(canonicalCity(option.city.value));
  assert.equal(canonicalCity('Лондон'), undefined);
  assert.equal(suggestCities('абракадабранеизвестныйгород').length, 0);
});
test('city data uses actual city name for federal and nested municipalities', () => {
  assert.equal(canonicalCity('Дмитров'), 'Дмитров');
  assert.equal(canonicalCity('Алупка'), 'Алупка');
  assert.equal(canonicalCity('Московская'), undefined);
  assert.equal(new Set(cities.map(city => city.value)).size, cities.length);
});
test('city validation also protects final review; online and any city bypass appropriately', () => {
  const draft = { current_step: 9, directions: ['frontend'] as const, preferred_format: 'offline' as const, preferred_cities: ['Мосвка'] };
  assert.ok(validation({ ...draft, directions: ['frontend'] }, [], {} as Questionnaire));
  assert.equal(validation({ ...draft, directions: ['frontend'], any_city: true }, [], {} as Questionnaire), null);
  assert.equal(cleanDraft({ ...draft, directions: ['frontend'], preferred_format: 'online' }).preferred_cities, undefined);
  assert.equal(suggestCities('Москва', ['Москва']).some(option => option.city.value === 'Москва'), false);
});

test('final review cannot complete with a legacy typo', () => {
  assert.ok(validation({ current_step: 60, directions: ['frontend'], goals: ['course'], education_stage: 'alumni', preferred_format: 'offline', preferred_cities: ['Мосвка'] }, [], {} as Questionnaire)?.includes('справочнике'));
});
