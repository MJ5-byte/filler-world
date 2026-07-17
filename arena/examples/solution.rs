use std::collections::VecDeque;
use std::io::{self, BufRead};

struct Game {
    player_num: u8,
    my_chars: (char, char),
    enemy_chars: (char, char),
    anfield: Vec<Vec<char>>,
    rows: usize,
    cols: usize,
    piece_blocks: Vec<(i32, i32)>,
    piece_rows: usize,
    piece_cols: usize,
    heat_map: Vec<Vec<i32>>,
    my_heat_map: Vec<Vec<i32>>,
    frontier_map: Vec<Vec<bool>>,
    turn_count: usize,
    my_cells: Vec<(usize, usize)>,
    enemy_cells: Vec<(usize, usize)>,
}

impl Game {
    fn new() -> Self {
        Game {
            player_num: 1,
            my_chars: ('@', 'a'),
            enemy_chars: ('$', 's'),
            anfield: Vec::new(),
            rows: 0,
            cols: 0,
            piece_blocks: Vec::new(),
            piece_rows: 0,
            piece_cols: 0,
            heat_map: Vec::new(),
            my_heat_map: Vec::new(),
            frontier_map: Vec::new(),
            turn_count: 0,
            my_cells: Vec::new(),
            enemy_cells: Vec::new(),
        }
    }

    fn parse_player_num(&mut self, line: &str) {
        if line.contains("p1") {
            self.player_num = 1;
            self.my_chars = ('@', 'a');
            self.enemy_chars = ('$', 's');
        } else {
            self.player_num = 2;
            self.my_chars = ('$', 's');
            self.enemy_chars = ('@', 'a');
        }
    }

    fn is_my_cell(&self, ch: char) -> bool {
        ch == self.my_chars.0 || ch == self.my_chars.1
    }

    fn is_enemy(&self, ch: char) -> bool {
        ch == self.enemy_chars.0 || ch == self.enemy_chars.1
    }

    fn read_anfield<R: BufRead>(&mut self, reader: &mut R) -> bool {
        let mut line = String::new();

        if reader.read_line(&mut line).unwrap_or(0) == 0 {
            return false;
        }

        if !line.starts_with("Anfield") {
            return false;
        }

        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() < 3 {
            return false;
        }

        self.cols = parts[1].parse().unwrap_or(0);
        self.rows = parts[2].trim_end_matches(':').parse().unwrap_or(0);

        line.clear();
        if reader.read_line(&mut line).unwrap_or(0) == 0 {
            return false;
        }

        self.anfield = Vec::with_capacity(self.rows);
        self.my_cells.clear();
        self.enemy_cells.clear();

        for r in 0..self.rows {
            line.clear();
            if reader.read_line(&mut line).unwrap_or(0) == 0 {
                return false;
            }

            let map_data = if let Some(idx) = line.find(' ') {
                &line[idx + 1..]
            } else {
                &line
            };

            let row: Vec<char> = map_data.trim_end().chars().collect();

            for (c, &ch) in row.iter().enumerate() {
                if self.is_my_cell(ch) {
                    self.my_cells.push((r, c));
                } else if self.is_enemy(ch) {
                    self.enemy_cells.push((r, c));
                }
            }

            self.anfield.push(row);
        }

        true
    }

    fn read_piece<R: BufRead>(&mut self, reader: &mut R) -> bool {
        let mut line = String::new();

        if reader.read_line(&mut line).unwrap_or(0) == 0 {
            return false;
        }

        if !line.starts_with("Piece") {
            return false;
        }

        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() < 3 {
            return false;
        }

        self.piece_cols = parts[1].parse().unwrap_or(0);
        self.piece_rows = parts[2].trim_end_matches(':').parse().unwrap_or(0);

        self.piece_blocks.clear();

        for row in 0..self.piece_rows {
            line.clear();
            if reader.read_line(&mut line).unwrap_or(0) == 0 {
                return false;
            }

            for (col, ch) in line.chars().enumerate() {
                if col < self.piece_cols && (ch == 'O' || ch == '#' || ch == '*') {
                    self.piece_blocks.push((row as i32, col as i32));
                }
            }
        }

        true
    }

    fn compute_distance_map(&self, starts: &[(usize, usize)]) -> Vec<Vec<i32>> {
        let max_dist = (self.rows + self.cols) as i32;
        let mut dist_map = vec![vec![max_dist; self.cols]; self.rows];
        let mut queue: VecDeque<(usize, usize)> = VecDeque::new();

        for &(r, c) in starts {
            if r < self.rows && c < self.cols {
                dist_map[r][c] = 0;
                queue.push_back((r, c));
            }
        }

        let dirs: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];

        while let Some((r, c)) = queue.pop_front() {
            let cur_dist = dist_map[r][c];

            for (dr, dc) in dirs.iter() {
                let nr = r as i32 + dr;
                let nc = c as i32 + dc;

                if nr >= 0 && nr < self.rows as i32 && nc >= 0 && nc < self.cols as i32 {
                    let nr = nr as usize;
                    let nc = nc as usize;
                    let new_dist = cur_dist + 1;

                    if new_dist < dist_map[nr][nc] {
                        dist_map[nr][nc] = new_dist;
                        queue.push_back((nr, nc));
                    }
                }
            }
        }

        dist_map
    }

    fn build_maps(&mut self) {
        self.heat_map = self.compute_distance_map(&self.enemy_cells);
        self.my_heat_map = self.compute_distance_map(&self.my_cells);

        self.frontier_map = vec![vec![false; self.cols]; self.rows];
        let dirs: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];

        for &(r, c) in &self.enemy_cells {
            for (dr, dc) in dirs.iter() {
                let nr = r as i32 + dr;
                let nc = c as i32 + dc;
                if nr >= 0 && nr < self.rows as i32 && nc >= 0 && nc < self.cols as i32 {
                    let nr = nr as usize;
                    let nc = nc as usize;
                    if nc < self.anfield[nr].len() && !self.is_enemy(self.anfield[nr][nc]) {
                        self.frontier_map[nr][nc] = true;
                    }
                }
            }
        }
    }

    fn can_place(&self, row: i32, col: i32) -> bool {
        let mut my_overlaps = 0;

        for &(br, bc) in &self.piece_blocks {
            let r = row + br;
            let c = col + bc;

            if r < 0 || r >= self.rows as i32 || c < 0 || c >= self.cols as i32 {
                return false;
            }

            let r = r as usize;
            let c = c as usize;

            if c >= self.anfield[r].len() {
                return false;
            }

            let ch = self.anfield[r][c];

            if self.is_enemy(ch) {
                return false;
            }
            if self.is_my_cell(ch) {
                my_overlaps += 1;
                if my_overlaps > 1 {
                    return false;
                }
            }
        }

        my_overlaps == 1
    }

    fn count_contested_cells(&self, row: i32, col: i32) -> i32 {
        let mut count = 0;
        for &(br, bc) in &self.piece_blocks {
            let r = (row + br) as usize;
            let c = (col + bc) as usize;

            if r < self.rows && c < self.cols {
                let enemy_dist = self.heat_map[r][c];
                let my_dist = self.my_heat_map[r][c];

                if enemy_dist <= my_dist {
                    count += (my_dist - enemy_dist + 1) * 2;
                }
            }
        }
        count
    }

    fn count_frontier_cells(&self, row: i32, col: i32) -> i32 {
        let mut count = 0;
        for &(br, bc) in &self.piece_blocks {
            let r = (row + br) as usize;
            let c = (col + bc) as usize;

            if r < self.rows && c < self.cols && self.frontier_map[r][c] {
                count += 1;
            }
        }
        count
    }

    fn heat_score(&self, row: i32, col: i32) -> i32 {
        let mut score = 0;
        for &(br, bc) in &self.piece_blocks {
            let r = (row + br) as usize;
            let c = (col + bc) as usize;
            if r < self.rows && c < self.cols {
                score += self.heat_map[r][c];
            }
        }
        score
    }

    fn count_enemy_adjacency(&self, row: i32, col: i32) -> i32 {
        let mut contact = 0;
        let dirs: [(i32, i32); 8] = [
            (-1, 0), (1, 0), (0, -1), (0, 1),
            (-1, -1), (-1, 1), (1, -1), (1, 1)
        ];

        for &(br, bc) in &self.piece_blocks {
            let r = row + br;
            let c = col + bc;

            for (dr, dc) in dirs.iter() {
                let nr = r + dr;
                let nc = c + dc;

                if nr >= 0 && nr < self.rows as i32 && nc >= 0 && nc < self.cols as i32 {
                    let nr = nr as usize;
                    let nc = nc as usize;
                    if nc < self.anfield[nr].len() && self.is_enemy(self.anfield[nr][nc]) {
                        contact += 1;
                    }
                }
            }
        }
        contact
    }

    fn territory_control_score(&self, row: i32, col: i32) -> i32 {
        let mut score = 0;

        for &(br, bc) in &self.piece_blocks {
            let r = (row + br) as usize;
            let c = (col + bc) as usize;

            if r < self.rows && c < self.cols {
                let enemy_dist = self.heat_map[r][c];
                let my_dist = self.my_heat_map[r][c];

                if enemy_dist < my_dist {
                    score += (my_dist - enemy_dist) * 3;
                }

                if enemy_dist <= 3 {
                    score += (4 - enemy_dist) * 2;
                }
            }
        }

        score
    }

    fn compactness_score(&self, row: i32, col: i32) -> i32 {
        let mut contact = 0;
        let dirs: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];

        for &(br, bc) in &self.piece_blocks {
            let r = row + br;
            let c = col + bc;

            for (dr, dc) in dirs.iter() {
                let nr = r + dr;
                let nc = c + dc;

                if nr >= 0 && nr < self.rows as i32 && nc >= 0 && nc < self.cols as i32 {
                    let nr = nr as usize;
                    let nc = nc as usize;
                    if nc < self.anfield[nr].len() && self.is_my_cell(self.anfield[nr][nc]) {
                        contact += 1;
                    }
                }
            }
        }
        contact
    }

    fn find_best_placement(&self) -> (i32, i32) {
        let mut best_pos: Option<(i32, i32)> = None;
        let mut best_score = i32::MIN;

        let row_start = 1 - self.piece_rows as i32;
        let col_start = 1 - self.piece_cols as i32;

        for row in row_start..self.rows as i32 {
            for col in col_start..self.cols as i32 {
                if !self.can_place(row, col) {
                    continue;
                }

                let heat = self.heat_score(row, col);
                let contested = self.count_contested_cells(row, col);
                let frontier = self.count_frontier_cells(row, col);
                let enemy_adj = self.count_enemy_adjacency(row, col);
                let territory = self.territory_control_score(row, col);
                let compact = self.compactness_score(row, col);

                // Winning formula - DO NOT CHANGE
                let score = contested * 100
                          + frontier * 80
                          + territory * 50
                          + enemy_adj * 30
                          + compact * 10
                          - heat * 5;

                if score > best_score {
                    best_score = score;
                    best_pos = Some((col, row));
                }
            }
        }

        best_pos.unwrap_or((0, 0))
    }
}

fn main() {
    let stdin = io::stdin();
    let mut reader = stdin.lock();
    let mut game = Game::new();

    let mut line = String::new();
    if reader.read_line(&mut line).unwrap_or(0) > 0 {
        game.parse_player_num(&line);
    }

    loop {
        if !game.read_anfield(&mut reader) {
            println!("0 0");
            break;
        }

        if !game.read_piece(&mut reader) {
            println!("0 0");
            break;
        }

        game.build_maps();
        let (col, row) = game.find_best_placement();
        println!("{} {}", col, row);
        game.turn_count += 1;
    }
}
