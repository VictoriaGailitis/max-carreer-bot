import { useEffect, useRef, useState } from 'react';
import { ApiError, type NavigatorClient, type DraftInput, type Questionnaire, type Question, type UserState, type Experience } from '../../api/client/client';
import { CityPicker } from './CityPicker';
import { completionKey } from './compat';
import { cityValidation } from './cities';
import { assessmentDraft, cleanDraft, labels, steps, toDraft, toggleExclusive, validation } from './model';
import { Button, Choice, ConfirmDialog, ErrorNotice, Heading, Mascot } from './ui';

export function Onboarding({ api, state, metadata, onComplete, onExit }: { api: NavigatorClient; state: UserState; metadata: Questionnaire; onComplete: () => void; onExit: () => void }) {
  const [draft, setDraft] = useState<DraftInput>(() => assessmentDraft(state));
  const revision = useRef(state.draft_revision);
  const completion = useRef<{ revision: number; key: string } | null>(null);
  const lock = useRef(false);
  const [questions, setQuestions] = useState<Question[]>([]);
  const [cityQuery, setCityQuery] = useState('');
  const [started, setStarted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [conflict, setConflict] = useState(false);
  const [reviewEdit, setReviewEdit] = useState(false);
  const [exitConfirm, setExitConfirm] = useState(false);
  const step = draft.current_step;
  useEffect(() => { document.querySelector<HTMLElement>('h1')?.focus({ preventScroll: true }); }, [step, started]);
  const route = steps(draft, questions);
  const question = questions[step - 11];
  function change(patch: Partial<DraftInput>) { setDraft(d => ({ ...d, ...patch })); setError(undefined); }
  async function run(work: () => Promise<void>) {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError(undefined);
    try { await work(); } catch (e) { setError(e); if (e instanceof ApiError && e.status < 500) completion.current = null; if (e instanceof ApiError && e.detail.code === 'REVISION_CONFLICT') setConflict(true); }
    finally { lock.current = false; setBusy(false); }
  }
  async function prepare() {
    await run(async () => {
      let next = cleanDraft(draft);
      if (next.directions.length) {
        const preview = await api.preview({ directions: next.directions, profiles: next.profiles });
        setQuestions(preview.questions);
        next = cleanDraft(next, preview.questions);
        if (next.current_step >= 11 && next.current_step < 60 && !preview.questions[next.current_step - 11]) next.current_step = 10;
      }
      if (next.preferred_format && next.preferred_format !== 'online' && !next.any_city && next.preferred_cities?.length && cityValidation(next.preferred_cities)) next.current_step = 9;
      setDraft(next); setStarted(true);
    });
  }
  async function advance() {
    if (step === 9 && !draft.any_city && cityQuery.trim()) { setError('Выбери введённый город из подсказок или очисти поле.'); return; }
    const invalid = validation(draft, questions, metadata);
    if (invalid) { setError(invalid); return; }
    await run(async () => {
      let current = cleanDraft(draft);
      let currentQuestions = questions;
      if ([1, 2, 3, 10].includes(step)) {
        const preview = await api.preview({ directions: current.directions, profiles: current.profiles });
        currentQuestions = preview.questions;
        setQuestions(currentQuestions);
        current = cleanDraft(current, currentQuestions);
      }
      if (step === 60) {
        if (!completion.current) {
          const saved = await api.saveDraft(revision.current, current);
          revision.current = saved.revision;
          completion.current = { revision: saved.revision, key: completionKey() };
        }
        await api.complete(completion.current.revision, completion.current.key);
        onComplete(); return;
      }
      const currentRoute = steps(current, currentQuestions);
      const nextStep = reviewEdit && step >= 11 ? 60 : currentRoute[currentRoute.indexOf(step) + 1];
      const next = { ...current, current_step: nextStep ?? 60 };
      const saved = await api.saveDraft(revision.current, next);
      revision.current = saved.revision;
      completion.current = null;
      setDraft(next); setReviewEdit(false);
      window.scrollTo({ top: 0 });
    });
  }
  async function useServerDraft() {
    await run(async () => {
      const latest = await api.draft();
      const next = toDraft(latest.draft);
      const preview = next.directions.length ? await api.preview({ directions: next.directions, profiles: next.profiles }) : null;
      revision.current = latest.revision; completion.current = null;
      setDraft(next); setQuestions(preview?.questions ?? []); setConflict(false); setReviewEdit(false);
    });
  }
  function back() {
    if (completion.current) return;
    const previous = reviewEdit ? 60 : route[route.indexOf(step) - 1];
    if (!previous) { setExitConfirm(true); return; }
    change({ current_step: previous }); setReviewEdit(false); window.scrollTo({ top: 0 });
  }
  const choiceList = (options: string[], selected: string | string[] | undefined, update: (v: string) => void) => <div className="choices">{options.map(value => <Choice key={value} checked={Array.isArray(selected) ? selected.includes(value) : selected === value} onChange={() => update(value)}>{labels[value] ?? value}</Choice>)}</div>;
  const titles: Record<number, string> = { 2: 'Что ближе\nв мобильной разработке?', 3: 'Что ближе\nв менеджменте?', 4: 'На чём\nуже пишешь?', 5: 'Что ты ищешь\nсейчас?', 6: 'На каком этапе\nобучения ты сейчас?', 7: 'На каком\nты курсе?', 8: 'Какой формат\nтебе подходит?', 9: 'Где ты можешь\nучаствовать очно?', 10: 'Не экзамен.\nТвой ориентир.', 60: 'Всё верно?' };
  const descriptions: Record<number, string> = { 2: 'Выбери один вариант. По нему подберём вопросы.', 3: 'Выбери один вариант. По нему подберём вопросы.', 4: 'Можно выбрать несколько. «Нет опыта» выбирается отдельно.', 5: 'Можно выбрать несколько целей. «Пока изучаю» — отдельный вариант.', 6: 'Это поможет учитывать условия участия.', 7: 'Необязательно. Можно не указывать.', 8: 'Учитываем это при подборе возможностей.', 9: 'Выбери один или несколько городов России. «Любой город» выбирается отдельно.', 10: 'Расскажи, что уже умеешь. Курсик поможет выбрать следующий шаг.', 60: 'Проверь ответы. Любой из них можно изменить.' };
  if (!started) return <main className="screen"><Heading eyebrow="ТВОЙ МАРШРУТ" title={state.draft ? 'Продолжим\nзнакомство?' : state.profile ? 'Новый взгляд\nна твой опыт' : 'Давай\nпознакомимся'}>{state.draft ? 'Сохранённый черновик ждёт тебя. Продолжим с того места, где остановились.' : 'Выбери интересные направления, расскажи о себе и оцени свой опыт.'}</Heading><div className="intro-art"><Mascot /></div><ErrorNotice error={error}/><Button disabled={busy} onClick={prepare}>{busy ? 'Загружаем…' : state.draft ? 'Продолжить черновик' : 'Выбрать направления'}</Button><button className="button secondary" onClick={onExit}>Назад</button></main>;
  return <main className={`screen onboarding ${step === 1 ? 'directions-screen' : ''} ${question ? 'question-screen' : ''} ${step === 10 ? 'assessment-intro' : ''}`}>
    <fieldset disabled={busy || conflict || !!completion.current} className="screen-fields">
      {step === 1 ? <><Heading title={'Что тебе\nинтересно?'}>Выбери до трёх направлений</Heading><progress max={5} value={1} aria-label="Настройка профиля"/><div className="direction-grid">{metadata.directions.map(d => {
        const checked = draft.directions.includes(d.id);
        return <button className={`direction-card ${checked ? 'selected' : ''}`} key={d.id} aria-pressed={checked} disabled={!checked && draft.directions.length >= 3} onClick={() => change({ directions: checked ? draft.directions.filter(v => v !== d.id) : [...draft.directions, d.id] })}><img src={`/assets/direction-${d.id}.svg`} alt=""/>{checked && <img className="selected-mark" src="/assets/selected.svg" alt=""/>}<span>{labels[d.id] ?? d.title}</span></button>;
      })}</div></> : question ? <><div className="question-top"><span className="pill">НАВЫКИ · {step - 10}/{questions.length}</span><progress max={questions.length} value={step - 10} aria-label="Прогресс самооценки"/></div><section className="question-card"><p className="eyebrow">{labels[question.direction_id]} · НАВЫК</p><h1>{question.text}</h1><p className="muted">Выбери, насколько уверенно ты это делаешь</p><div className="choices">{metadata.answer_scale.map(answer => <Choice key={String(answer.value)} checked={Object.prototype.hasOwnProperty.call(draft.skills ?? {}, question.id) && draft.skills![question.id] === answer.value} onChange={() => change({ skills: { ...draft.skills, [question.id]: answer.value as Experience } })}>{answer.label}</Choice>)}</div></section><Mascot key={question.id} pose={(['think', 'wave', 'support'] as const)[(step - 11) % 3]} className={`question-mascot position-${(step - 11) % 3}`}/></> : <><Heading eyebrow="НАСТРОЙКА ТВОЕГО КУРСА" title={titles[step] ?? 'Твой опыт'}>{descriptions[step]}</Heading>
      {(step === 2 || step === 3) && <div className="choices option-panel">{metadata.profiles.filter(p => p.direction_id === (step === 2 ? 'mobile' : 'management')).map(p => {
        const key = step === 2 ? 'mobile' : 'management';
        return <Choice key={p.id} checked={(draft.profiles?.[key] ?? (key === 'mobile' ? 'mobile-all' : 'product')) === p.id} onChange={() => change({ profiles: { ...draft.profiles, [key]: p.id } })}>{p.title}</Choice>;
      })}</div>}
      {step === 4 && choiceList(metadata.backend_languages, draft.backend_languages, value => change({ backend_languages: toggleExclusive(draft.backend_languages ?? [], value, 'none') }))}
      {step === 5 && <div className="option-panel">{choiceList(metadata.goals, draft.goals, value => change({ goals: toggleExclusive(draft.goals ?? [], value as NonNullable<DraftInput['goals']>[number], 'explore') }))}</div>}
      {step === 6 && choiceList(metadata.education_stages, draft.education_stage, value => change({ education_stage: value as DraftInput['education_stage'], study_year: undefined }))}
      {step === 7 && <div className="choices">{[1,2,3,4,5,6].map(year => <Choice key={year} checked={draft.study_year === year} onChange={() => change({ study_year: year })}>{year} курс</Choice>)}<Choice checked={!draft.study_year} onChange={() => change({ study_year: undefined })}>Не указывать</Choice></div>}
      {step === 8 && choiceList(metadata.preferred_formats, draft.preferred_format, value => change({ preferred_format: value as DraftInput['preferred_format'] }))}
      {step === 9 && <CityPicker selected={draft.preferred_cities ?? []} query={cityQuery} anyCity={!!draft.any_city} onQuery={setCityQuery} onChange={(cities, anyCity) => change({ preferred_cities: cities, any_city: anyCity, other_city: undefined })}/>}
      {step === 10 && <div className="intro-panel"><div className="intro-illustration"><Mascot pose="think"/></div><p className="intro-hint">Сомневаешься? Выбирай<br/>«Не могу оценить»</p></div>}
      {step === 60 && <div className="review-list">{[
        [1, 'Направления', draft.directions.map(v => labels[v]).join(', ')], [5, 'Цель', draft.goals?.map(v => labels[v]).join(', ')], [6, 'Обучение', labels[draft.education_stage ?? '']], [8, 'Формат', labels[draft.preferred_format ?? '']],
        ...(draft.preferred_format !== 'online' ? [[9, 'Города', draft.any_city ? 'Любой город' : draft.preferred_cities?.join(', ')]] : []),
        ...(draft.directions.includes('backend') ? [[4, 'Языки', draft.backend_languages?.map(v => labels[v] ?? v).join(', ')]] : []),
      ].map(([target, label, value]) => <button className="review-row" key={target} onClick={() => change({ current_step: Number(target) })}><span>{label}<strong>{value}</strong></span><span aria-hidden="true">↗</span></button>)}<h2>Твоя самооценка</h2>{questions.map((q, i) => <button className="review-row" key={q.id} onClick={() => { change({ current_step: 11 + i }); setReviewEdit(true); }}><span>{q.text}<strong>{metadata.answer_scale.find(a => a.value === draft.skills?.[q.id])?.label ?? 'Нет ответа'}</strong></span><span aria-hidden="true">↗</span></button>)}</div>}
      </>}
    </fieldset>
    <ErrorNotice error={error}/>
    {conflict && <section className="notice"><p>Можно продолжить с серверной версией. Это заменит несохранённые ответы на этом экране.</p><Button disabled={busy} onClick={useServerDraft}>Загрузить серверную версию</Button></section>}
    <footer className="onboarding-footer"><Button disabled={busy || conflict || (step === 1 && !draft.directions.length)} onClick={advance}>{busy ? 'Сохраняем…' : step === 60 ? completion.current ? 'Повторить завершение' : 'Показать результаты' : step === 10 ? 'Начать самооценку' : question ? reviewEdit ? 'Сохранить ответ' : 'Следующий вопрос' : `Продолжить${step === 1 ? ` · ${draft.directions.length}` : ''}`}</Button><div className="footer-links"><button className="button secondary" disabled={busy || !!completion.current} onClick={back}>← Назад</button><button className="button secondary" disabled={busy || !!completion.current} onClick={() => { if (cityQuery.trim() && !draft.any_city) { setError('Выбери введённый город из подсказок или очисти поле перед выходом.'); return; } setExitConfirm(true); }}>Позже</button></div></footer>
    {exitConfirm && <ConfirmDialog title="Выйти из опроса" busy={busy} onClose={() => setExitConfirm(false)}><h2>Продолжим позже?</h2><p>Сохраним текущие ответы в черновике.</p><Button disabled={busy || conflict} onClick={() => run(async () => { const saved = await api.saveDraft(revision.current, cleanDraft(draft)); revision.current = saved.revision; onExit(); })}>Сохранить и выйти</Button><button className="button secondary" disabled={busy} onClick={() => setExitConfirm(false)}>Остаться в опросе</button><ErrorNotice error={error}/></ConfirmDialog>}
  </main>;
}
