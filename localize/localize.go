package localize

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

type AssetLoader func(path string) ([]byte, error)

type Strings map[string]string

type StringsSet map[string]Strings

type Localizer struct {
	loadAsset  AssetLoader
	lang       string
	stringsSet StringsSet
}

func NewLocalizer(loadAsset AssetLoader) (*Localizer, error) {
	l := &Localizer{
		loadAsset:  loadAsset,
		lang:       "en",
		stringsSet: make(StringsSet),
	}
	err := l.LoadLocale("en")
	if err != nil {
		return nil, err
	}

	return l, nil
}

func (l *Localizer) SetLang(lang string) {
	log.Println("Switching to lang", lang)
	l.lang = lang
}

func (l *Localizer) LoadLocale(locale string) error {
	locale = strings.Replace(locale, "-", "_", -1)

	assetPath := fmt.Sprintf("data/locales/%s.json", locale)
	log.Println("Trying to load locale", locale)

	localeBytes, err := l.loadAsset(assetPath)
	if err != nil {
		log.Println("While looking for locale file", locale, err.Error())
		return err
	}

	strings := Strings{}
	err = json.Unmarshal(localeBytes, &strings)
	if err != nil {
		log.Println("While parsing locale file", locale, err.Error())
		return err
	}

	l.stringsSet[locale] = strings

	return nil
}

type Replacements map[string]string

func (l *Localizer) T(key string, args ...Replacements) string {
	for _, lang := range []string{l.lang, "en"} {
		ss := l.stringsSet[lang]
		rule, ok := ss[key]
		if !ok {
			continue
		}

		result := rule
		if len(args) > 0 {
			for k, v := range args[0] {
				result = strings.Replace(result, "{{"+k+"}}", v, -1)
			}
		}

		return result
	}

	return key
}
