// Development-only preview. No authentication, persistence or requests to the backend.
import bank from '../../backend/data/question-bank.v1.json';
import { NavigatorClient, type Questionnaire, type Selection, type Preview, type DraftInput, type UserState, type Profile, type AssessmentResult, type Direction, type Experience, type Opportunity, type CatalogResult, type FavoritesResult } from '../../api/client/client';

export class DemoClient extends NavigatorClient {
  private state: UserState = { profile: null, profile_revision: 0, draft: null, draft_revision: 0 };
  private saved = new Set<string>();
  override async me() { return structuredClone(this.state); }
  override async questionnaire(): Promise<Questionnaire> {
    return { questionnaire_version: bank.questionnaire_version, selection_version: 1,
      directions: bank.directions as Questionnaire['directions'], profiles: bank.profiles as Questionnaire['profiles'], answer_scale: bank.answer_scale as Questionnaire['answer_scale'],
      goals: ['course','internship','event','explore'], education_stages: ['vocational','undergraduate','graduate','alumni','other','prefer_not_to_say'], preferred_formats: ['online','offline','both'],
      backend_languages: ['go','python','java','javascript','typescript','csharp','cpp','php','ruby','rust','kotlin','other','none'], general_questions: [] };
  }
  override async preview(selection: Selection): Promise<Preview> {
    const count = [0,8,12,16][selection.directions.length] ?? 8;
    const questions = selection.directions.flatMap((direction, index) => {
      const specialization = direction === 'mobile' ? selection.profiles?.mobile : direction === 'management' ? selection.profiles?.management : undefined;
      const profileIds = bank.profiles.find(p => p.id === specialization)?.question_ids ?? [];
      const pool = bank.questions.filter(q => q.direction_id === direction && (!q.profile_id || profileIds.includes(q.id)));
      return pool.slice(0, Math.floor(count / selection.directions.length) + (index < count % selection.directions.length ? 1 : 0)).map(q => ({ id: q.id, text: q.text, direction_id: q.direction_id as Direction }));
    });
    return { questionnaire_version: bank.questionnaire_version, selection_version: 1, directions: selection.directions as Direction[], profiles: selection.profiles, question_count: questions.length as Preview['question_count'], questions };
  }
  override async saveDraft(_revision: number, draft: DraftInput) {
    const preview = await this.preview(draft);
    this.state.draft = { ...structuredClone(draft), schema_version: 1, questionnaire_version: bank.questionnaire_version, selection_version: 1, question_ids: preview.questions.map(q => q.id) };
    this.state.draft_revision++;
    return this.draft();
  }
  override async draft() { return { draft: structuredClone(this.state.draft), revision: this.state.draft_revision }; }
  override async complete() {
    if (!this.state.draft) throw new Error('No demo draft');
    const { current_step: _step, ...document } = this.state.draft;
    this.state.profile = { ...document, skills: this.state.draft.skills ?? {}, completed_at: new Date().toISOString() } as Profile;
    this.state.profile_revision++; this.state.draft = null;
    return { profile: this.state.profile, revision: this.state.profile_revision };
  }
  override async results(): Promise<AssessmentResult> {
    const profile = this.state.profile!;
    const questions = (await this.preview(profile)).questions;
    return { kind: 'self_assessment', scoring_version: 1, questionnaire_version: bank.questionnaire_version, profile_revision: this.state.profile_revision, completed_at: profile.completed_at, evaluated_at: new Date().toISOString(),
      disclaimer: 'Демонстрация. Это субъективная самооценка, а не экзамен, диагностика или гарантия готовности к стажировке.',
      directions: (profile.directions as Direction[]).map(id => {
        const skills = questions.filter(q => q.direction_id === id).map(q => ({ question_id: q.id, text: q.text, answer: profile.skills[q.id] ?? null as Experience, answer_label: bank.answer_scale.find(a => a.value === profile.skills[q.id])?.label ?? 'Не могу оценить' }));
        const assessed = skills.filter(s => s.answer !== null);
        const score = assessed.length ? Math.round(assessed.reduce((sum, s) => sum + (s.answer ?? 0), 0) / assessed.length / 3 * 100) : 0;
        const level = score < 25 ? 'starter' : score < 50 ? 'developing' : score < 75 ? 'solid' : 'confident';
        return { direction_id: id, title: bank.directions.find(d => d.id === id)!.title, score_percent: score, level, level_label: { starter: 'Можно начать с основ', developing: 'Основа уже складывается', solid: 'Есть хорошая база', confident: 'Уверенная база' }[level], description: assessed.length ? 'Выбери следующий шаг и продолжай развивать свой опыт.' : 'Пока нет самооценки. Это не означает, что у тебя нет знаний.', basis: assessed.length ? 'self_reported' : 'no_self_assessment', assessed_count: assessed.length, question_count: skills.length, coverage_percent: Math.round(assessed.length / skills.length * 100), skills, focus_skills: [...assessed].sort((a,b) => a.answer! - b.answer!).slice(0,3), unassessed_skill_ids: skills.filter(s => s.answer === null).map(s => s.question_id), course_filter: { type: 'course', direction: id } };
      }) };
  }
  override async opportunities(filters: { type?: string; direction?: string; format?: string } = {}): Promise<CatalogResult> {
    return { catalog_version: 'frontend-demo', profile_revision: this.state.profile_revision, ranking_version: 1, evaluated_at: new Date().toISOString(), has_direction_matches: true,
      items: demoItems.filter(item => (!filters.type || item.type === filters.type) && (!filters.direction || item.directions.includes(filters.direction as Direction)) && (!filters.format || item.format === filters.format)) };
  }
  override async opportunity(id: string) { const catalog = await this.opportunities(); return { ...catalog, item: demoItems.find(item => item.id === id)! }; }
  override async favorites(): Promise<FavoritesResult> { const catalog = await this.opportunities(); return { ...catalog, favorites_revision: 1, items: demoItems.filter(item => this.saved.has(item.id)).map(item => ({ id: item.id, effective_status: item.effective_status, item })) }; }
  override async addFavorite(id: string) { this.saved.add(id); }
  override async removeFavorite(id: string) { this.saved.delete(id); }
  override async logout() {}
  override async deleteData() { this.state = { profile: null, draft: null, profile_revision: 0, draft_revision: 0 }; this.saved.clear(); }
}
const demoItems: Opportunity[] = [
  { id: '244', title: 'Базовый Python', directions: ['backend'] },
  { id: '509', title: 'Базовый CSS', directions: ['frontend'] },
].map(item => ({ ...item, directions: item.directions as Direction[], type: 'course', organizer_id: 'VK Education', summary: 'Демонстрационная карточка из макета', description: 'Демонстрационная карточка для проверки интерфейса. Актуальную программу и условия смотри на сайте организатора.', format: 'online', cities: null, education_requirements: null, requirements_text: null, skill_requirements: null, no_prerequisites: null, details: { course: { price_text: 'Бесплатно' } }, starts_at: null, ends_at: null, deadline_at: null, source_url: `https://education.vk.company/program/${item.id}`, action_url: `https://education.vk.company/program/${item.id}`, checked_at: '2026-09-30T00:00:00Z', publication_status: 'published', availability: 'unknown', is_demo: true, effective_status: 'active', availability_hint: 'check_source', reasons: [] }));
