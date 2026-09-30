import assert from 'node:assert/strict';
import test from 'node:test';
import { assessmentDraft, cleanDraft, steps, toDraft, toggleExclusive, validation } from '../src/model.ts';
import type { DraftInput, Questionnaire } from '../../api/client/client.ts';
const metadata = {} as Questionnaire;
const questions = [{ id: 'BACK-01', text: 'Навык', direction_id: 'backend' as const }];
test('exclusive answers never coexist with other selections', () => {
  assert.deepEqual(toggleExclusive(['course', 'event'], 'explore', 'explore'), ['explore']);
  assert.deepEqual(toggleExclusive(['explore'], 'course', 'explore'), ['course']);
  assert.deepEqual(toggleExclusive(['none'], 'go', 'none'), ['go']);
});
test('null is an answered question, absent key is not', () => {
  const draft: DraftInput = { current_step: 11, directions: ['backend'], skills: { 'BACK-01': null } };
  assert.equal(validation(draft, questions, metadata), null);
  assert.ok(validation({ ...draft, skills: {} }, questions, metadata));
});
test('changing selection removes invalid conditional answers and obsolete skills', () => {
  const draft: DraftInput = { current_step: 1, directions: ['frontend'], profiles: { mobile: 'ios' }, backend_languages: ['go'], preferred_format: 'online', any_city: true, preferred_cities: ['Москва'], education_stage: 'alumni', study_year: 2, skills: { 'BACK-01': 2, 'OLD-01': 1 } };
  const clean = cleanDraft(draft, questions);
  assert.equal(clean.backend_languages, undefined);
  assert.equal(clean.profiles?.mobile, undefined);
  assert.equal(clean.preferred_cities, undefined);
  assert.equal(clean.study_year, undefined);
  assert.deepEqual(clean.skills, { 'BACK-01': 2 });
  assert.deepEqual(draft.backend_languages, ['go']);
});
test('routes omit irrelevant questions and retain stable resume indices', () => {
  assert.deepEqual(steps({ current_step: 1, directions: ['frontend'], education_stage: 'alumni', preferred_format: 'online' }, questions), [1,5,6,8,10,11,60]);
  assert.ok(steps({ current_step: 1, directions: ['mobile'], education_stage: 'undergraduate', preferred_format: 'offline' }, questions).includes(9));
});
test('editing a profile creates a copy with only input fields', () => {
  const profile = { directions: ['backend'], skills: { 'BACK-01': 3 }, completed_at: 'today', schema_version: 1, questionnaire_version: 'v1', selection_version: 1, question_ids: ['BACK-01'] } as const;
  const draft = toDraft(structuredClone(profile) as never);
  draft.skills!['BACK-01'] = null;
  assert.equal(profile.skills['BACK-01'], 3);
  assert.equal('completed_at' in draft, false);
  assert.equal(draft.current_step, 1);
});
test('retake clears completed answers, preserves preferences and does not mutate the profile', () => {
  const profile = { directions: ['backend'], goals: ['course'], skills: { 'BACK-01': 3 }, completed_at: 'today' } as const;
  const draft = assessmentDraft({ profile: profile as never, draft: null });
  assert.deepEqual(draft.skills, {});
  assert.equal(draft.current_step, 1);
  assert.deepEqual(draft.goals, ['course']);
  assert.equal(profile.skills['BACK-01'], 3);
});
test('retake resumes an existing draft including explicit unknown answers', () => {
  const saved = { current_step: 12, directions: ['backend'], skills: { 'BACK-01': null } };
  const draft = assessmentDraft({ profile: null, draft: saved as never });
  assert.equal(draft.current_step, 12);
  assert.deepEqual(draft.skills, { 'BACK-01': null });
  draft.skills!['BACK-01'] = 3;
  assert.equal(saved.skills['BACK-01'], null);
});
