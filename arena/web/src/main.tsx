import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import Landing from './pages/Landing'
import Leaderboard from './pages/Leaderboard'
import Upload from './pages/Upload'
import BotDetail from './pages/BotDetail'
import Matches from './pages/Matches'
import ReplayViewer from './pages/ReplayViewer'
import Players from './pages/Players'
import PlayerProfile from './pages/PlayerProfile'
import Challenge from './pages/Challenge'
import Tournaments from './pages/Tournaments'
import TournamentDetail from './pages/TournamentDetail'
import Admin from './pages/Admin'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Landing />} />
        {/* Pathless layout route: applies the sidebar shell to these
            absolute paths without itself claiming "/" (which Landing owns). */}
        <Route element={<App />}>
          <Route path="/leaderboard" element={<Leaderboard />} />
          <Route path="/upload" element={<Upload />} />
          <Route path="/bots/:id" element={<BotDetail />} />
          <Route path="/matches" element={<Matches />} />
          <Route path="/matches/:id" element={<ReplayViewer />} />
          <Route path="/players" element={<Players />} />
          <Route path="/players/:name" element={<PlayerProfile />} />
          <Route path="/challenge" element={<Challenge />} />
          <Route path="/tournaments" element={<Tournaments />} />
          <Route path="/tournaments/:id" element={<TournamentDetail />} />
          <Route path="/admin" element={<Admin />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
