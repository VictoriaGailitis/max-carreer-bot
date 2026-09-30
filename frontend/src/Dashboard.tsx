import { useEffect, useRef, useState, type ReactNode } from 'react';
import { type NavigatorClient, type AssessmentResult, type DirectionResult, type Opportunity, type FavoritesResult, type UserState, type Direction, type OpportunityType, type Format } from '../../api/client/client';
import { Button, Choice, ConfirmDialog, ErrorNotice, Heading, Loading, Mascot, StatusPanel } from './ui';
import { labels } from './model';
import { openExternal } from './bridge';

type Tab = 'results' | 'catalog' | 'favorites' | 'profile';
const tabs: { id: Tab; title: string }[] = [{ id: 'results', title: 'Твой курс' }, { id: 'catalog', title: 'Каталог' }, { id: 'favorites', title: 'Избранное' }, { id: 'profile', title: 'Профиль' }];
const categories: { type?: OpportunityType; title: string }[] = [{ title: 'Все' }, { type: 'course', title: 'Курсы' }, { type: 'internship', title: 'Стажировки' }, { type: 'event', title: 'Мероприятия' }];
const directions: Direction[] = ['backend', 'frontend', 'mobile', 'devops', 'ml', 'analytics', 'security', 'management'];
const openLabels: Record<OpportunityType, string> = { course: 'Открыть курс', internship: 'Открыть стажировку', event: 'Открыть мероприятие' };

export function Dashboard({ api, state, onEdit, onLogout }: { api: NavigatorClient; state: UserState; onEdit: () => void; onLogout: () => void }) {
  const [tab, setTab] = useState<Tab>('results');
  const [results, setResults] = useState<AssessmentResult>();
  const [items, setItems] = useState<Opportunity[]>([]);
  const [favorites, setFavorites] = useState<FavoritesResult['items']>([]);
  const [favoritesKnown, setFavoritesKnown] = useState(false);
  const [selectedDirection, setSelectedDirection] = useState<Direction>();
  const direction = results?.directions.find(d => d.direction_id === selectedDirection) ?? results?.directions[0];
  const [detail, setDetail] = useState<Opportunity>();
  const [privacy, setPrivacy] = useState(false);
  const [filters, setFilters] = useState<{ type?: OpportunityType; format?: Format }>({});
  const [selectedDirections, setSelectedDirections] = useState<Direction[]>([]);
  const visibleItems = items.filter(item => !selectedDirections.length || item.directions.some(d => selectedDirections.includes(d)));
  const filterCount = selectedDirections.length + Number(!!filters.format);
  const [busy, setBusy] = useState(true);
  const [actionBusy, setActionBusy] = useState(false);
  const actionLock = useRef(false);
  const [error, setError] = useState<unknown>();
  const [actionError, setActionError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  const [notice, setNotice] = useState('');
  const [confirm, setConfirm] = useState<'logout' | 'delete' | null>(null);
  const [showFilters, setShowFilters] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    setBusy(true); setError(undefined);
    const load = async () => {
      if (tab === 'results') { const data = await api.results(controller.signal); if (!controller.signal.aborted) setResults(data); }
      if (tab === 'catalog') { const data = await api.opportunities(filters, controller.signal); if (!controller.signal.aborted) setItems(data.items); }
      if (tab === 'favorites') { const data = await api.favorites(controller.signal); if (!controller.signal.aborted) { setFavorites(data.items); setFavoritesKnown(true); } }
    };
    load().catch(e => { if (!controller.signal.aborted) setError(e); }).finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
  }, [api, tab, filters, reload]);

  // Saved status loads independently: its failure must not hide the assessment.
  useEffect(() => {
    const controller = new AbortController();
    api.favorites(controller.signal).then(data => {
      if (!controller.signal.aborted) { setFavorites(data.items); setFavoritesKnown(true); }
    }).catch(() => { /* Unknown status stays disabled; the Favorites tab offers a retry. */ });
    return () => controller.abort();
  }, [api]);

  async function action(work: () => Promise<void>) {
    if (actionLock.current) return;
    actionLock.current = true; setActionBusy(true); setActionError(undefined); setNotice('');
    try { await work(); } catch (e) { setActionError(e); } finally { actionLock.current = false; setActionBusy(false); }
  }
  const navigate = (next: Tab) => {
    if (actionLock.current) return;
    setTab(next); setDetail(undefined); setPrivacy(false); setActionError(undefined); setNotice(''); setError(undefined); window.scrollTo({ top: 0 });
  };
  const open = (item: Opportunity) => action(async () => { const response = await api.opportunity(item.id); setDetail(response.item); window.scrollTo({ top: 0 }); });
  const isSaved = (id: string) => favorites.some(f => f.id === id);
  const remove = (id: string) => action(async () => {
    await api.removeFavorite(id); setFavorites(list => list.filter(v => v.id !== id)); setNotice('Убрали из избранного');
  });
  const toggleFavorite = (item: Opportunity) => isSaved(item.id) ? remove(item.id) : action(async () => {
    await api.addFavorite(item.id);
    setFavorites(list => [...list.filter(f => f.id !== item.id), { id: item.id, effective_status: item.effective_status, item }]);
    setNotice('Сохранено в избранное');
  });
  const card = (item: Opportunity, index: number, track = false) => <OpportunityCard key={item.id} item={item} featured={!track && index === 0} track={track} trackDirection={track ? direction?.direction_id : undefined} saved={isSaved(item.id)} showFavorite={track || tab === 'favorites'} disabled={actionBusy} favoriteDisabled={!favoritesKnown || actionBusy || (tab === 'favorites' && busy)} onOpen={() => open(item)} onFavorite={() => toggleFavorite(item)}/>;
  function switchDirection(offset: number) {
    if (!results || !direction) return;
    const index = results.directions.indexOf(direction);
    setSelectedDirection(results.directions[(index + offset + results.directions.length) % results.directions.length].direction_id);
    setNotice(''); setActionError(undefined);
  }
  let content: ReactNode;
  if (detail) content = <>
    <button className="text-button back" onClick={() => { setDetail(undefined); setNotice(''); setActionError(undefined); }}>← Назад</button>
    <Heading eyebrow={labels[detail.type]} title={detail.title}>{detail.organizer_id} · {labels[detail.format]}</Heading>
    <div className="tag-row">{detail.directions.map(d => <span className="tag" key={d}>{labels[d]}</span>)}</div>
    <section className="content-card lilac"><h2>О возможности</h2><p className="pre-wrap">{detail.description}</p>{detail.requirements_text && <><h2>Условия участия</h2><p>{detail.requirements_text}</p></>}{detail.deadline_at && <p>Приём заявок до {new Date(detail.deadline_at).toLocaleDateString('ru')}</p>}</section>
    {detail.availability_hint === 'check_source' && <p className="muted">Актуальность набора уточняй на сайте организатора.</p>}
    <Button disabled={detail.effective_status === 'closed' || actionBusy} onClick={() => { try { openExternal(detail.action_url); } catch { setActionError('Не удалось открыть ссылку. Попробуй ещё раз.'); } }}>{detail.effective_status === 'closed' ? 'Набор закрыт' : 'Перейти на сайт'}</Button>
    <Button className="secondary" disabled={actionBusy || !favoritesKnown || (!isSaved(detail.id) && detail.effective_status === 'closed')} onClick={() => toggleFavorite(detail)}>{actionBusy ? 'Подожди…' : isSaved(detail.id) ? 'Убрать из избранного' : 'Сохранить в избранное'}</Button>
  </>;
  else if (tab === 'results') content = <>
    <p className="eyebrow">ТВОЙ КУРС</p>
    {direction && <>
      <div className="direction-switcher"><button aria-label="Предыдущее направление" disabled={actionBusy || results!.directions.length < 2} onClick={() => switchDirection(-1)}>←</button><h1 aria-live="polite">{labels[direction.direction_id]}</h1><button aria-label="Следующее направление" disabled={actionBusy || results!.directions.length < 2} onClick={() => switchDirection(1)}>→</button></div>
      <p className="muted">Не оценка знаний. Ориентир для роста.</p>
      <section className="result-hero" aria-label="Результат самооценки"><img className="result-icon" src={`/assets/direction-${direction.direction_id}.svg`} alt=""/><Mascot pose="support"/><h2>{direction.level_label}</h2><p>{direction.description}</p></section>
      {direction.assessed_count < direction.question_count && <p className="muted disclaimer">{direction.basis === 'no_self_assessment' ? 'Пока начнём с основ: ты не оценил навыки в этом направлении.' : 'Ориентир по оценённым навыкам. Ответы «Не могу оценить» не учитывались.'}</p>}
      <CourseSection key={direction.direction_id} api={api} direction={direction} renderCard={(item, index) => card(item, index, true)}/>
    </>}
    {!busy && !error && !direction && <Empty title="Пока нет результатов">Пройди тест, чтобы найти свой ориентир.<Button onClick={onEdit}>Пройти тест</Button></Empty>}
  </>;
  else if (tab === 'catalog') content = <>
    <div className="catalog-heading"><Heading eyebrow="КАТАЛОГ ВОЗМОЖНОСТЕЙ" title={'Есть куда\nрасти'}/><button className="filter-toggle" onClick={() => setShowFilters(v => !v)} aria-label="Фильтры" aria-expanded={showFilters} aria-controls="catalog-filters"><svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3"/><circle cx="15" cy="17" r="3"/></svg>{filterCount > 0 && <span className="filter-count">{filterCount}</span>}</button></div>
    <div className="category-tabs" aria-label="Тип возможности">{categories.map(c => <button key={c.type ?? 'all'} aria-pressed={filters.type === c.type} onClick={() => { setItems([]); setFilters(v => ({ ...v, type: c.type })); }}>{c.title}</button>)}</div>
    {showFilters && <section className="filters" id="catalog-filters" aria-label="Фильтры каталога"><fieldset className="direction-filters"><legend>Направления</legend><p>Можно выбрать несколько</p><div>{directions.map(value => <Choice key={value} checked={selectedDirections.includes(value)} onChange={() => setSelectedDirections(current => current.includes(value) ? current.filter(d => d !== value) : [...current, value])}>{labels[value]}</Choice>)}</div></fieldset><label>Формат<select value={filters.format ?? ''} onChange={e => { setItems([]); setFilters(v => ({ ...v, format: e.target.value as Format || undefined })); }}><option value="">Любой формат</option>{['online','offline','hybrid','unknown'].map(v => <option key={v} value={v}>{labels[v]}</option>)}</select></label><Button className="secondary" onClick={() => { setFilters(v => ({ type: v.type })); setSelectedDirections([]); }}>Сбросить фильтры</Button></section>}
    <div className="opportunity-list">{visibleItems.map((item, index) => card(item, index))}</div>
    {!busy && !error && !visibleItems.length && <Empty title="Пока ничего не нашлось">Попробуй изменить фильтры или загляни позже.</Empty>}
    <Button className="secondary" onClick={() => setReload(v => v + 1)} disabled={busy}>Обновить каталог</Button>
  </>;
  else if (tab === 'favorites') content = <>
    <Heading eyebrow="ИЗБРАННОЕ" title={'Всегда\nпод рукой'}>Всё, к чему хочется вернуться.</Heading>
    <div className="opportunity-list">{favorites.map((favorite, index) => favorite.item ? card(favorite.item, index) : <section key={favorite.id} className="content-card opportunity-card has-favorite"><h2>Карточка больше недоступна</h2><p>Её можно убрать из избранного.</p><FavoriteButton saved disabled={actionBusy} onClick={() => remove(favorite.id)}/></section>)}</div>
    {!busy && !error && !favorites.length && <Empty title="Здесь будет твоё избранное">Сохраняй интересные возможности, чтобы вернуться к ним позже.</Empty>}
    <Button onClick={() => navigate('catalog')}>Найти ещё что-нибудь</Button>
  </>;
  else if (privacy) content = <>
    <button className="text-button back" onClick={() => setPrivacy(false)}>← Профиль</button><h1 className="privacy-title">Данные и приватность</h1>
    <section className="settings-card"><h2>Что сохраняется</h2><p>Выбранные направления и предпочтения, ответы на тест, черновик и избранное.<br/><br/>Учётная запись связана с твоим ID в MAX.</p></section>
    <button className="settings-card" onClick={() => setConfirm('logout')}><h2>Выйти из приложения →</h2><p>Завершить текущую сессию.<br/>Профиль и избранное сохранятся.</p></button>
    <button className="settings-card" onClick={() => setConfirm('delete')}><h2 className="danger">Удалить мои данные →</h2><p>Удалить профиль, ответы, черновик и избранное. Сначала — подтверждение.</p></button>
  </>;
  else content = <>
    <Heading title="Профиль"/>
    <section className="profile-hero"><p className="eyebrow">ТВОИ НАПРАВЛЕНИЯ</p><h2>{state.profile?.directions.map(d => <span key={d}>{labels[d]}</span>)}</h2><Mascot pose="support"/></section>
    <div className="profile-preferences"><div><span>ХОЧУ НАЙТИ</span><strong>{state.profile?.goals?.map(g => labels[g]).join(', ') || 'Не указано'}</strong></div><div><span>УДОБНЫЙ ФОРМАТ</span><strong>{labels[state.profile?.preferred_format ?? ''] || 'Не указан'}</strong></div></div>
    <button className="settings-card privacy-link" onClick={() => setPrivacy(true)}><h2>Данные и приватность →</h2><p>Управление данными и доступом</p></button>
    <button className="settings-card lilac" onClick={onEdit}><h2>{state.draft ? 'Продолжить тест ↗' : 'Пройти тест заново ↗'}</h2><p>{state.draft ? 'Твой черновик сохранён. Продолжи с того места, где остановился.' : 'Интересы меняются — и это нормально. Обнови свой ориентир, когда захочешь.'}</p></button>
  </>;
  const fullStatus = !detail && ((tab === 'results' && !direction) || (tab === 'catalog' && !items.length) || (tab === 'favorites' && !favorites.length)) && (busy || !!error);
  return <main className="screen dashboard">{fullStatus ? <StatusPanel title={busy ? 'Собираем твой ориентир' : 'Пока не удалось загрузить'} loading={busy} error={error} retry={error ? () => setReload(v => v + 1) : undefined}>{!!error && <Button className="secondary" onClick={() => navigate('profile')}>Открыть профиль</Button>}</StatusPanel> : content}{!fullStatus && busy && <Loading/>}{!fullStatus && <ErrorNotice error={error} retry={() => setReload(v => v + 1)}/>}<ErrorNotice error={actionError}/>{notice && <p className="success" role="status">{notice}</p>}
    <nav className="bottom-nav" aria-label="Основная навигация">{tabs.map(t => <button key={t.id} disabled={actionBusy} onClick={() => navigate(t.id)} aria-current={tab === t.id ? 'page' : undefined}><img src={`/assets/nav-${t.id}.svg`} alt=""/><span>{t.title}</span></button>)}</nav>
    {confirm && <ConfirmDialog title={confirm === 'delete' ? 'Подтвердить удаление' : 'Подтвердить выход'} busy={actionBusy} onClose={() => setConfirm(null)}><h2>{confirm === 'delete' ? 'Удалить все данные?' : 'Выйти из приложения?'}</h2><p>{confirm === 'delete' ? 'Профиль, ответы, черновик и избранное будут удалены. Восстановить их не получится.' : 'Профиль и избранное сохранятся. Ты сможешь войти снова через MAX.'}</p><Button disabled={actionBusy} onClick={() => setConfirm(null)}>{confirm === 'delete' ? 'Оставить мои данные' : 'Остаться'}</Button><Button className={`secondary ${confirm === 'delete' ? 'danger' : ''}`} disabled={actionBusy} onClick={() => action(async () => { if (confirm === 'delete') await api.deleteData(); else await api.logout(); onLogout(); })}>{actionBusy ? 'Подожди…' : confirm === 'delete' ? 'Удалить данные навсегда' : 'Выйти'}</Button><ErrorNotice error={actionError}/></ConfirmDialog>}
  </main>;
}

function FavoriteButton({ saved, disabled, onClick }: { saved: boolean; disabled?: boolean; onClick: () => void }) {
  return <button className="favorite-button" aria-label={saved ? 'Убрать из избранного' : 'Сохранить в избранное'} aria-pressed={saved} disabled={disabled} onClick={onClick}><img src="/assets/nav-favorites.svg" alt=""/></button>;
}

function OpportunityCard({ item, featured, track, trackDirection, saved, showFavorite, disabled, favoriteDisabled, onOpen, onFavorite }: { item: Opportunity; featured: boolean; track: boolean; trackDirection?: Direction; saved: boolean; showFavorite: boolean; disabled: boolean; favoriteDisabled: boolean; onOpen: () => void; onFavorite: () => void }) {
  return <article className={`content-card opportunity-card ${featured ? 'dark featured' : track ? 'lilac track-card' : ''} ${showFavorite ? 'has-favorite' : ''}`}>
    {featured && <div className="card-heading"><img className="card-icon" src={`/assets/direction-${item.directions[0] ?? 'backend'}.svg`} alt=""/><span className="tag">{labels[item.type]}</span></div>}
    <h2>{item.title}</h2>
    {track && <p>Курс по направлению «{labels[trackDirection ?? item.directions[0] ?? 'backend']}»</p>}
    <p>{item.organizer_id} · {labels[item.format]}{item.details.course?.price_text ? ` · ${item.details.course.price_text}` : ''}</p>
    {item.effective_status === 'closed' && <p>Набор закрыт</p>}
    {item.is_demo && <small>Демо</small>}
    <button className={`card-action ${featured || track ? 'lime' : ''}`} disabled={disabled} onClick={onOpen}>{showFavorite ? openLabels[item.type] : 'Подробнее'} ↗</button>
    {showFavorite && <FavoriteButton saved={saved} disabled={favoriteDisabled || (!saved && item.effective_status === 'closed')} onClick={onFavorite}/>}
  </article>;
}

function Empty({ title, children }: { title: string; children: ReactNode }) {
  return <section className="content-card lilac empty-state"><Mascot pose="think"/><h2>{title}</h2><div>{children}</div></section>;
}

function CourseSection({ api, direction, renderCard }: { api: NavigatorClient; direction: DirectionResult; renderCard: (item: Opportunity, index: number) => ReactNode }) {
  const [items, setItems] = useState<Opportunity[]>([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setBusy(true); setError(undefined); setItems([]);
    api.opportunities(direction.course_filter, controller.signal).then(data => { if (!controller.signal.aborted) setItems(data.items); }).catch(e => { if (!controller.signal.aborted) setError(e); }).finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
  }, [api, direction, reload]);
  return <section className="course-section"><h2 className="next-step-title">Твой следующий шаг</h2>{busy && <Loading/>}<ErrorNotice error={error} retry={() => setReload(v => v + 1)}/>{items.map(renderCard)}{!busy && !error && !items.length && <Empty title="Курсы ещё появятся">Твои результаты уже сохранены. Загляни сюда позже.</Empty>}</section>;
}
