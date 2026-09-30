import assert from 'node:assert/strict';
import test from 'node:test';
import { completionKey } from '../src/compat.ts';
import { cleanDraft, validation } from '../src/model.ts';
import type { DraftInput, Questionnaire } from '../../api/client/client.ts';

test('start and answer preparation work without modern WebView APIs', () => {
  const clone = Object.getOwnPropertyDescriptor(globalThis, 'structuredClone')!;
  const own = Object.getOwnPropertyDescriptor(Object, 'hasOwn')!;
  const uuid = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(crypto), 'randomUUID')!;
  try {
    Object.defineProperty(globalThis, 'structuredClone', { value: undefined, configurable: true });
    Object.defineProperty(Object, 'hasOwn', { value: undefined, configurable: true });
    Object.defineProperty(Object.getPrototypeOf(crypto), 'randomUUID', { value: undefined, configurable: true });
    assert.deepEqual(cleanDraft({ current_step: 1, directions: [], skills: {} }), { current_step: 1, directions: [], skills: {} });
    const draft: DraftInput = { current_step: 11, directions: ['backend'], skills: { q1: null, old: 2 }, profiles: { mobile: 'ios' } };
    const questions = [{ id: 'q1', text: 'Question', direction_id: 'backend' as const }];
    const clean = cleanDraft(draft, questions);
    assert.deepEqual(clean.skills, { q1: null });
    assert.equal(validation(clean, questions, {} as Questionnaire), null);
    assert.ok(validation({ ...clean, skills: {} }, questions, {} as Questionnaire));
    assert.equal(draft.profiles?.mobile, 'ios');
    assert.equal(draft.skills?.old, 2);
    const first = completionKey();
    assert.match(first, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.notEqual(completionKey(), first);
  } finally {
    Object.defineProperty(globalThis, 'structuredClone', clone);
    Object.defineProperty(Object, 'hasOwn', own);
    Object.defineProperty(Object.getPrototypeOf(crypto), 'randomUUID', uuid);
  }
});
