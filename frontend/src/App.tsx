import { useState } from 'react';
import { NavigatorClient, type Questionnaire, type UserState } from '../../api/client/client';
import { loadBridge } from './bridge';
import { Dashboard } from './Dashboard';
import { Onboarding } from './Onboarding';
import { Button, ErrorNotice, Heading, Mascot } from './ui';

const liveApi = new NavigatorClient();
export function App() {
  const [api, setApi] = useState(liveApi);
  const [state, setState] = useState<UserState>();
  const [metadata, setMetadata] = useState<Questionnaire>();
  const [screen, setScreen] = useState<'welcome' | 'login' | 'onboarding' | 'dashboard'>('welcome');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [demo, setDemo] = useState(false);
  const [devInitData, setDevInitData] = useState('');
  const [showDev, setShowDev] = useState(false);
  async function load(client: NavigatorClient, edit = false) {
    const [nextState, nextMetadata] = await Promise.all([client.me(), client.questionnaire()]);
    setState(nextState); setMetadata(nextMetadata);
    setScreen(edit || !nextState.profile ? 'onboarding' : 'dashboard');
  }
  async function login() {
    if (busy) return;
    setBusy(true); setError(undefined);
    try {
      if (!(import.meta.env.DEV && devInitData)) await loadBridge();
      const initData = import.meta.env.DEV && devInitData ? devInitData : window.WebApp?.initData;
      if (!initData) throw new Error('NO_MAX');
      await liveApi.login(initData); setDevInitData('');
      window.WebApp?.ready?.(); setApi(liveApi); setDemo(false); await load(liveApi);
    } catch (e) { setError(e); setScreen('login'); } finally { setBusy(false); }
  }
  async function startDemo() {
    if (busy) return;
    setBusy(true); setError(undefined);
    try {
      if (import.meta.env.DEV) {
        const { DemoClient } = await import('./demo');
        const client = new DemoClient(); setApi(client); setDemo(true); await load(client);
      }
    } catch (e) { setError(e); } finally { setBusy(false); }
  }
  async function refresh(edit = false) {
    setBusy(true); setError(undefined);
    try { await load(api, edit); } catch (e) { setError(e); } finally { setBusy(false); }
  }
  return <div className="app-shell">{demo && <aside className="demo-banner">ДЕМО · данные только в памяти, без сохранения на сервере</aside>}
    {screen === 'welcome' ? <main className="welcome"><div className="welcome-art"><span className="brand-pill">НАВИГАТОР ВОЗМОЖНОСТЕЙ</span><img className="orbit" src="/assets/orbit-route.svg" alt=""/><Mascot/><span className="pixel"/></div><section className="welcome-card"><h1>Найди<br/>свой маршрут</h1><p>Расскажи о своих интересах.<br/>Курсик поможет найти возможности!</p><div className="welcome-action"><Button onClick={login} disabled={busy}>{busy ? 'Подключаемся…' : 'Начать знакомство'}</Button><small>Можно вернуться и пройти заново</small></div></section></main>
    : screen === 'login' ? <main className="screen"><Heading eyebrow="ВХОД В ПРИЛОЖЕНИЕ" title={'Давай снова\nпознакомимся'}>Для сохранения ответов открой Курсика в MAX.</Heading><div className="intro-art"><Mascot/></div><ErrorNotice error={error}/><Button disabled={busy} onClick={login}>{busy ? 'Подключаемся…' : 'Повторить вход'}</Button>{import.meta.env.DEV && <><Button className="secondary" disabled={busy} onClick={startDemo}>Посмотреть демо</Button><button className="text-button" onClick={() => setShowDev(v => !v)}>Локальный вход для разработки</button>{showDev && <label className="field">Синтетический initData<textarea value={devInitData} onChange={e => setDevInitData(e.target.value)} autoComplete="off" spellCheck={false}/></label>}</>}<button className="button secondary" onClick={() => { setScreen('welcome'); setError(undefined); }}>← На главную</button></main>
    : state && metadata && (screen === 'onboarding' ? <Onboarding api={api} state={state} metadata={metadata} onComplete={() => { void refresh(); }} onExit={() => { if (state.profile) void refresh(); else { setScreen('welcome'); setError(undefined); } }}/>
    : <Dashboard api={api} state={state} onEdit={() => { void refresh(true); }} onLogout={() => { setState(undefined); setScreen('welcome'); setDemo(false); }}/>) }
    {screen !== 'login' && !!error && <div className="global-error"><ErrorNotice error={error} retry={() => { void refresh(); }}/></div>}
    {screen === 'welcome' && import.meta.env.DEV && <aside className="dev-tools"><button onClick={startDemo} disabled={busy}>Открыть демо без MAX ↗</button></aside>}
  </div>;
}
