import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import Leaderboard from './pages/Leaderboard'
import Upload from './pages/Upload'
import BotDetail from './pages/BotDetail'
import Matches from './pages/Matches'
import ReplayViewer from './pages/ReplayViewer'
import Players from './pages/Players'
import PlayerProfile from './pages/PlayerProfile'
import Challenge from './pages/Challenge'
import Login from './pages/Login'
import Admin from './pages/Admin'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Leaderboard />} />
          <Route path="upload" element={<Upload />} />
          <Route path="bots/:id" element={<BotDetail />} />
          <Route path="matches" element={<Matches />} />
          <Route path="matches/:id" element={<ReplayViewer />} />
          <Route path="players" element={<Players />} />
          <Route path="players/:name" element={<PlayerProfile />} />
          <Route path="challenge" element={<Challenge />} />
          <Route path="login" element={<Login />} />
          <Route path="admin" element={<Admin />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
