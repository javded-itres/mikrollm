package guard

func init() {
	Register(Plugin{
		ID: "nsfw", Label: "NSFW (explicit)", Group: "nsfw",
		Words: []string{
			"nsfw", "porn", "porno", "pornography", "xxx video", "xxx videos",
			"hentai", "onlyfans", "rule34", "rule 34",
			"generate porn", "write porn", "porn story", "erotic story",
			"explicit sex scene", "hardcore porn", "nude photo", "nude photos",
			"naked pictures", "send nudes", "sex tape",
			"порно", "порнограф", "хентай", "эротический рассказ", "порно рассказ",
			"сгенерируй порно", "напиши порно", "голое фото", "голые фото",
			"nsfw картинк", "18+ видео",
		},
	})
	Register(Plugin{
		ID: "adult", Label: "Для взрослых (18+)", Group: "nsfw",
		Words: []string{
			"adult content", "adult video", "adult videos", "erotic roleplay",
			"erotica", "sex chat", "sexting", "camgirl", "strip club",
			"escort service", "sex toy", "sex toys",
			"для взрослых", "контент 18+", "эротическ", "секс чат",
			"ролеплей 18", "интим услу", "стриптиз",
		},
	})
	Register(Plugin{
		ID: "csam", Label: "CSAM / несовершеннолетние", Group: "nsfw",
		Words: []string{
			"child porn", "child pornography", "csam", "детская порнография",
			"child sexual", "underage porn", "loli porn",
		},
	})
	Register(Plugin{
		ID: "violence", Label: "Насилие", Group: "harmful",
		Words: []string{"how to kill", "make a bomb", "изготовление бомбы", "как убить человека"},
	})
	Register(Plugin{
		ID: "self_harm", Label: "Суицид", Group: "harmful",
		Words: []string{"kill myself", "suicide methods", "способы суицида", "как покончить с собой"},
	})
	Register(Plugin{
		ID: "hate", Label: "Ненависть", Group: "harmful",
		Words: []string{"kill all the", "ethnic cleansing", "этническая чистка"},
	})
	Register(Plugin{
		ID: "weapons", Label: "Оружие", Group: "harmful",
		Words: []string{"build a bomb", "3d printed gun", "рецепт взрывчатки", "самодельная бомба"},
	})
	Register(Plugin{
		ID: "drugs", Label: "Наркотики", Group: "harmful",
		Words: []string{"cook meth", "how to synthesize", "варка мета"},
	})
}
