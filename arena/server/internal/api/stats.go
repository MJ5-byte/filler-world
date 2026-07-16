package api

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"filler-arena/internal/elo"
)

type ratingPoint struct {
	N      int     `json:"n"` // nth finished match of this bot
	Rating float64 `json:"rating"`
	At     string  `json:"at"`
}

type ratingSeries struct {
	BotID   int64         `json:"botId"`
	BotName string        `json:"botName"`
	Points  []ratingPoint `json:"points"`
}

type mapStat struct {
	Map    string `json:"map"`
	Wins   int    `json:"wins"`
	Losses int    `json:"losses"`
	Draws  int    `json:"draws"`
}

type rival struct {
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Wins   int    `json:"wins"`   // player's wins vs this bot
	Losses int    `json:"losses"` // player's losses vs this bot
}

type dayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type playerStats struct {
	RatingHistory []ratingSeries `json:"ratingHistory"`
	PerMap        []mapStat      `json:"perMap"`
	Nemesis       *rival         `json:"nemesis"`
	Prey          *rival         `json:"prey"`
	StreakCurrent int            `json:"streakCurrent"` // >0 wins in a row, <0 losses
	StreakBest    int            `json:"streakBest"`
	Domination    *float64       `json:"domination"` // avg share of total score
	Activity      []dayCount     `json:"activity"`   // last 14 days
	TotalMatches  int            `json:"totalMatches"`
}

func (s *Server) playerStatsHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx := r.Context()

	var userID int64
	err := s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE name=$1`, name).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "player not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats := playerStats{RatingHistory: []ratingSeries{}, PerMap: []mapStat{}, Activity: []dayCount{}}

	// --- rating history: replay every finished match through Elo, in order,
	// and record the trajectory of this player's bots. This reconstructs the
	// exact path the live ratings took without needing a snapshot table.
	myBots := map[int64]string{}
	rows, err := s.Pool.Query(ctx, `SELECT id, name FROM bots WHERE owner_id=$1`, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err == nil {
			myBots[id] = n
		}
	}
	rows.Close()

	rows, err = s.Pool.Query(ctx, `
		SELECT bot_a_id, bot_b_id, winner_id, finished_at
		FROM matches WHERE status='finished' ORDER BY finished_at, id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ratings := map[int64]float64{}
	series := map[int64]*ratingSeries{}
	get := func(id int64) float64 {
		if v, ok := ratings[id]; ok {
			return v
		}
		return 1200
	}
	for rows.Next() {
		var a, b int64
		var winner *int64
		var at time.Time
		if err := rows.Scan(&a, &b, &winner, &at); err != nil {
			continue
		}
		scoreA := 0.5
		if winner != nil && *winner == a {
			scoreA = 1
		} else if winner != nil {
			scoreA = 0
		}
		na, nb := elo.Update(get(a), get(b), scoreA)
		ratings[a], ratings[b] = na, nb
		for _, id := range []int64{a, b} {
			if _, mine := myBots[id]; !mine {
				continue
			}
			sr, ok := series[id]
			if !ok {
				sr = &ratingSeries{BotID: id, BotName: myBots[id]}
				series[id] = sr
			}
			sr.Points = append(sr.Points, ratingPoint{
				N: len(sr.Points) + 1, Rating: ratings[id], At: at.Format(time.RFC3339),
			})
		}
	}
	rows.Close()
	for _, sr := range series {
		stats.RatingHistory = append(stats.RatingHistory, *sr)
	}
	// most matches first; cap at 3 series so the chart stays readable
	sort.Slice(stats.RatingHistory, func(i, j int) bool {
		return len(stats.RatingHistory[i].Points) > len(stats.RatingHistory[j].Points)
	})
	if len(stats.RatingHistory) > 3 {
		stats.RatingHistory = stats.RatingHistory[:3]
	}

	// --- one pass over the player's finished matches for everything else
	rows, err = s.Pool.Query(ctx, `
		SELECT m.name,
		       CASE WHEN mt.winner_id = pb.id THEN 1
		            WHEN mt.winner_id IS NULL THEN 0
		            ELSE -1 END,
		       ob.name, COALESCE(ou.name, ''),
		       CASE WHEN mt.bot_a_id = pb.id THEN mt.score_a ELSE mt.score_b END,
		       CASE WHEN mt.bot_a_id = pb.id THEN mt.score_b ELSE mt.score_a END,
		       mt.finished_at
		FROM matches mt
		JOIN bots pb ON pb.owner_id = $1 AND pb.id IN (mt.bot_a_id, mt.bot_b_id)
		JOIN bots ob ON ob.id = (CASE WHEN mt.bot_a_id = pb.id THEN mt.bot_b_id ELSE mt.bot_a_id END)
		LEFT JOIN users ou ON ou.id = ob.owner_id
		JOIN maps m ON m.id = mt.map_id
		WHERE mt.status = 'finished'
		  -- matches between two of the player's own bots would count as both
		  -- a win and a loss and poison streaks/rivals; leave them out
		  AND ob.owner_id IS DISTINCT FROM $1
		ORDER BY mt.finished_at, mt.id`, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	perMap := map[string]*mapStat{}
	rivals := map[string]*rival{}
	byDay := map[string]int{}
	var domSum float64
	var domN int
	cur, best := 0, 0
	for rows.Next() {
		var mapName, oppName, oppOwner string
		var outcome int
		var myScore, oppScore *int
		var at time.Time
		if err := rows.Scan(&mapName, &outcome, &oppName, &oppOwner, &myScore, &oppScore, &at); err != nil {
			continue
		}
		stats.TotalMatches++

		ms, ok := perMap[mapName]
		if !ok {
			ms = &mapStat{Map: mapName}
			perMap[mapName] = ms
		}
		rv, ok := rivals[oppName]
		if !ok {
			rv = &rival{Name: oppName, Owner: oppOwner}
			rivals[oppName] = rv
		}
		switch outcome {
		case 1:
			ms.Wins++
			rv.Wins++
			if cur < 0 {
				cur = 0
			}
			cur++
			if cur > best {
				best = cur
			}
		case -1:
			ms.Losses++
			rv.Losses++
			if cur > 0 {
				cur = 0
			}
			cur--
		default:
			ms.Draws++
			cur = 0
		}
		if myScore != nil && oppScore != nil && *myScore+*oppScore > 0 {
			domSum += float64(*myScore) / float64(*myScore+*oppScore)
			domN++
		}
		byDay[at.Format("2006-01-02")]++
	}

	for _, ms := range perMap {
		stats.PerMap = append(stats.PerMap, *ms)
	}
	sort.Slice(stats.PerMap, func(i, j int) bool { return stats.PerMap[i].Map < stats.PerMap[j].Map })

	for _, rv := range rivals {
		if rv.Losses > 0 && (stats.Nemesis == nil || rv.Losses > stats.Nemesis.Losses) {
			c := *rv
			stats.Nemesis = &c
		}
		if rv.Wins > 0 && (stats.Prey == nil || rv.Wins > stats.Prey.Wins) {
			c := *rv
			stats.Prey = &c
		}
	}

	stats.StreakCurrent, stats.StreakBest = cur, best
	if domN > 0 {
		d := domSum / float64(domN)
		stats.Domination = &d
	}
	for i := 13; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		stats.Activity = append(stats.Activity, dayCount{Day: day, Count: byDay[day]})
	}

	writeJSON(w, http.StatusOK, stats)
}
