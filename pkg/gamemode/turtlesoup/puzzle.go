package turtlesoup

type Puzzle struct {
	ID       string   `json:"id"`
	Surface  string   `json:"surface"`
	Solution string   `json:"solution"`
	Hints    []string `json:"hints,omitempty"`
}

func DefaultPuzzles() []Puzzle {
	return []Puzzle{
		{
			ID:       "elevator-eight",
			Surface:  "一名男子每天都搭電梯到十樓。某天電梯停在八樓時，他突然哭了。",
			Solution: "男子是視障者。平日他的妻子會在八樓進電梯，陪他並替他確認十樓按鍵。那天八樓沒有人進來，他意識到妻子可能出事了，所以哭了。",
			Hints: []string{
				"關鍵不在電梯故障，而在八樓平常會發生的事。",
				"男子不是因為自己到不了十樓而哭。",
				"八樓和一位固定出現的人有關。",
			},
		},
		{
			ID:       "silent-phone",
			Surface:  "她把新買的手機放進冰箱。隔天早上，她看到手機沒有任何未接來電，反而鬆了一口氣。",
			Solution: "她懷疑有人偷偷用這支手機定位或聯絡她。她把手機放進冰箱，是為了暫時阻隔訊號並測試對方是否真的知道這支手機。整晚沒有人找來或打來，讓她確認自己暫時安全。",
			Hints: []string{
				"冰箱不是為了降溫或保存手機。",
				"她在測試某種外部聯繫是否存在。",
				"她害怕的不是手機壞掉，而是有人能透過手機找到她。",
			},
		},
		{
			ID:       "blank-exam",
			Surface:  "一名學生考試交了白卷。老師看完後，給了他滿分。",
			Solution: "這是一場觀察力測驗。題目要求學生在試卷上不要留下任何字跡，因為真正要測的是能否看清並遵守第一行指示。那名學生完全照做，所以得了滿分。",
			Hints: []string{
				"這不是老師偏心，也不是試卷上已經有答案。",
				"滿分來自他遵守了某個要求。",
				"白卷本身就是正確作答方式。",
			},
		},
	}
}
