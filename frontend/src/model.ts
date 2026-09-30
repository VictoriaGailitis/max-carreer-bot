import { canonicalCity, cityValidation } from './cities.ts';
import type { DraftInput, Questionnaire, Question, SavedDraft, Profile } from '../../api/client/client';
export const labels: Record<string, string> = {
  backend: 'Бэкенд', frontend: 'Фронтенд', mobile: 'Мобильная разработка', devops: 'DevOps', ml: 'ML', analytics: 'Аналитика', security: 'Безопасность', management: 'Менеджмент',
  course: 'Курсы', internship: 'Стажировки', event: 'События', explore: 'Пока изучаю возможности',
  vocational: 'Колледж / техникум', undergraduate: 'Бакалавриат / специалитет', graduate: 'Магистратура', alumni: 'Уже выпустился', other: 'Другое', prefer_not_to_say: 'Предпочитаю не отвечать',
  online: 'Онлайн', offline: 'Очно', both: 'Подойдут оба формата', hybrid: 'Гибридный', unknown: 'Формат уточняется',
  go: 'Go', python: 'Python', java: 'Java', javascript: 'JavaScript', typescript: 'TypeScript', csharp: 'C#', cpp: 'C++', php: 'PHP', ruby: 'Ruby', rust: 'Rust', kotlin: 'Kotlin', none: 'Нет опыта',
};
export function toggleExclusive<T extends string>(values: T[], value: T, exclusive: T): T[] {
  if (values.includes(value)) return values.filter(v => v !== value);
  return value === exclusive ? [value] : [...values.filter(v => v !== exclusive), value];
}
export function toDraft(value?: SavedDraft | Profile | null): DraftInput {
  return {
    current_step: value && !('completed_at' in value) && 'current_step' in value ? value.current_step : 1,
    directions: value?.directions ?? [], profiles: value?.profiles,
    goals: value?.goals, education_stage: value?.education_stage, study_year: value?.study_year,
    preferred_format: value?.preferred_format, preferred_cities: value?.other_city ? [...(value.preferred_cities ?? []), value.other_city] : value?.preferred_cities,
    any_city: value?.any_city, backend_languages: value?.backend_languages,
    skills: { ...value?.skills },
  };
}
// A completed assessment is retaken in full; an unfinished draft is resumed unchanged.
export function assessmentDraft(state: { draft: SavedDraft | null; profile: Profile | null }): DraftInput {
  if (state.draft) return toDraft(state.draft);
  return { ...toDraft(state.profile), current_step: 1, skills: {} };
}
export function steps(draft: DraftInput, questions: Question[]): number[] {
  return [1, ...(draft.directions.includes('mobile') ? [2] : []), ...(draft.directions.includes('management') ? [3] : []), ...(draft.directions.includes('backend') ? [4] : []), 5, 6,
    ...(['vocational', 'undergraduate', 'graduate'].includes(draft.education_stage ?? '') ? [7] : []), 8,
    ...(draft.preferred_format && draft.preferred_format !== 'online' ? [9] : []), 10, ...questions.map((_, i) => 11 + i), 60];
}
export function cleanDraft(draft: DraftInput, questions?: Question[]): DraftInput {
  const copy = structuredClone(draft);
  if (copy.preferred_cities) copy.preferred_cities = [...new Set(copy.preferred_cities.map(city => canonicalCity(city) ?? city.trim()).filter(Boolean))];
  if (!copy.directions.includes('backend')) delete copy.backend_languages;
  if (!copy.directions.includes('mobile') && copy.profiles) delete copy.profiles.mobile;
  if (!copy.directions.includes('management') && copy.profiles) delete copy.profiles.management;
  if (!['vocational', 'undergraduate', 'graduate'].includes(copy.education_stage ?? '')) delete copy.study_year;
  if (copy.preferred_format === 'online') { delete copy.preferred_cities; delete copy.any_city; delete copy.other_city; }
  if (copy.any_city) { delete copy.preferred_cities; delete copy.other_city; }
  if (questions) copy.skills = Object.fromEntries(questions.filter(q => Object.hasOwn(copy.skills ?? {}, q.id)).map(q => [q.id, copy.skills![q.id]]));
  return copy;
}
export function validation(draft: DraftInput, questions: Question[], metadata: Questionnaire): string | null {
  const step = draft.current_step;
  if (step === 1 && (draft.directions.length < 1 || draft.directions.length > 3)) return 'Выбери от одного до трёх направлений.';
  if (step === 4 && !draft.backend_languages?.length) return 'Выбери язык или «Нет опыта».';
  if (step === 5 && !draft.goals?.length) return 'Выбери хотя бы одну цель.';
  if (step === 6 && !draft.education_stage) return 'Выбери этап обучения.';
  if (step === 8 && !draft.preferred_format) return 'Выбери формат.';
  if (step === 9 && !draft.any_city) { const cityError = cityValidation(draft.preferred_cities); if (cityError) return cityError; }
  if (step >= 11 && step < 60) {
    const q = questions[step - 11];
    if (!q || !Object.hasOwn(draft.skills ?? {}, q.id)) return 'Выбери ответ, в том числе можно «Не могу оценить».';
  }
  if (step === 60) {
    for (const check of steps(draft, questions).filter(s => s !== 60)) {
      const error = validation({ ...draft, current_step: check }, questions, metadata);
      if (error) return error;
    }
  }
  return null;
}
