package tui

import (
	"strings"
	"time"
)

// A quote a day under the banner. Mostly Stoic, not only Stoic. Each is
// attributed to who actually said or wrote it; where a line is a modern
// summary of someone's thought, the summariser gets the credit.
type quote struct{ text, who string }

var quotes = []quote{
	{"Waste no more time arguing about what a good man should be. Be one.", "Marcus Aurelius"},
	{"The best revenge is not to be like that.", "Marcus Aurelius"},
	{"Confine yourself to the present.", "Marcus Aurelius"},
	{"If it is not right, do not do it; if it is not true, do not say it.", "Marcus Aurelius"},
	{"Very little is needed to make a happy life; it is all within yourself, in your way of thinking.", "Marcus Aurelius"},
	{"Never let the future disturb you. You will meet it, if you have to, with the same weapons of reason which today arm you against the present.", "Marcus Aurelius"},
	{"Look well into thyself; there is a source of strength which will always spring up if thou wilt always look.", "Marcus Aurelius"},
	{"How much time he gains who does not look to see what his neighbour says or does or thinks.", "Marcus Aurelius"},
	{"Loss is nothing else but change, and change is Nature's delight.", "Marcus Aurelius"},
	{"The impediment to action advances action. What stands in the way becomes the way.", "Marcus Aurelius"},
	{"Think of yourself as dead. You have lived your life. Now take what's left and live it properly.", "Marcus Aurelius"},
	{"It is not that we have a short time to live, but that we waste a lot of it.", "Seneca"},
	{"We suffer more often in imagination than in reality.", "Seneca"},
	{"Begin at once to live, and count each separate day as a separate life.", "Seneca"},
	{"As long as you live, keep learning how to live.", "Seneca"},
	{"No man was ever wise by chance.", "Seneca"},
	{"Difficulties strengthen the mind, as labour does the body.", "Seneca"},
	{"While we are postponing, life speeds by.", "Seneca"},
	{"Associate with people who are likely to improve you.", "Seneca"},
	{"Life is long if you know how to use it.", "Seneca"},
	{"He who has learned how to die has unlearned how to be a slave.", "Seneca"},
	{"There is no easy way from the earth to the stars.", "Seneca"},
	{"Men are disturbed not by things, but by the views which they take of things.", "Epictetus"},
	{"No man is free who is not master of himself.", "Epictetus"},
	{"First say to yourself what you would be; and then do what you have to do.", "Epictetus"},
	{"If you want to improve, be content to be thought foolish and stupid.", "Epictetus"},
	{"Wealth consists not in having great possessions, but in having few wants.", "Epictetus"},
	{"Make the best use of what is in your power, and take the rest as it happens.", "Epictetus"},
	{"It is impossible for a man to learn what he thinks he already knows.", "Epictetus"},
	{"Seek not that the things which happen should happen as you wish; but wish the things which happen to be as they are, and you will have a tranquil flow of life.", "Epictetus"},
	{"No great thing is created suddenly.", "Epictetus"},
	{"Only the educated are free.", "Epictetus"},
	{"Well-being is realised by small steps, but is truly no small thing.", "Zeno of Citium"},
	{"Not what we have but what we enjoy constitutes our abundance.", "Epicurus"},
	{"No man ever steps in the same river twice.", "Heraclitus"},
	{"The unexamined life is not worth living.", "Socrates"},
	{"We are what we repeatedly do. Excellence, then, is not an act but a habit.", "Will Durant, on Aristotle"},
	{"If you have a garden and a library, you have everything you need.", "Cicero"},
	{"A journey of a thousand miles begins with a single step.", "Lao Tzu"},
	{"Knowing others is intelligence; knowing yourself is true wisdom.", "Lao Tzu"},
	{"It does not matter how slowly you go as long as you do not stop.", "Confucius"},
	{"To study the self is to forget the self.", "Dōgen"},
	{"Do not seek to follow in the footsteps of the wise; seek what they sought.", "Matsuo Bashō"},
	{"Do nothing which is of no use.", "Miyamoto Musashi"},
	{"The wound is the place where the Light enters you.", "Rumi"},
	{"The greatest thing in the world is to know how to belong to oneself.", "Michel de Montaigne"},
	{"All of humanity's problems stem from man's inability to sit quietly in a room alone.", "Blaise Pascal"},
	{"Life can only be understood backwards; but it must be lived forwards.", "Søren Kierkegaard"},
	{"He who has a why to live for can bear almost any how.", "Friedrich Nietzsche"},
	{"Everyone thinks of changing the world, but no one thinks of changing himself.", "Leo Tolstoy"},
	{"It is not enough to be busy. So are the ants. The question is: what are we busy about?", "Henry David Thoreau"},
	{"Finish each day and be done with it. You have done what you could.", "Ralph Waldo Emerson"},
	{"Do not wait to strike till the iron is hot; but make it hot by striking.", "W. B. Yeats"},
	{"Ever tried. Ever failed. No matter. Try again. Fail again. Fail better.", "Samuel Beckett"},
	{"If we winter this one out, we can summer anywhere.", "Seamus Heaney"},
	{"How we spend our days is, of course, how we spend our lives.", "Annie Dillard"},
	{"Tell me, what is it you plan to do with your one wild and precious life?", "Mary Oliver"},
	{"Nothing will work unless you do.", "Maya Angelou"},
	{"Until you make the unconscious conscious, it will direct your life and you will call it fate.", "Carl Jung"},
	{"Smile, breathe and go slowly.", "Thích Nhất Hạnh"},
	{"Almost everything will work again if you unplug it for a few minutes, including you.", "Anne Lamott"},
}

// QuoteFor is deterministic per day and cycles through the list without
// repeating on the same date two years running.
func QuoteFor(t time.Time) (string, string) {
	i := (t.YearDay() + t.Year()*7) % len(quotes)
	q := quotes[i]
	return q.text, q.who
}

// wrap breaks text into lines no wider than w runes, on spaces.
func wrap(text string, w int) []string {
	if w < 10 {
		w = 10
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line == "" {
			line = word
			continue
		}
		if len([]rune(line))+1+len([]rune(word)) > w {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// wrapBalanced wraps like wrap but then narrows the width as far as it can
// without adding a line, so the lines come out roughly even instead of
// one long line and a dangling word.
func wrapBalanced(text string, w int) []string {
	lines := wrap(text, w)
	if len(lines) < 2 {
		return lines
	}
	for narrow := w - 1; narrow >= 10; narrow-- {
		if len(wrap(text, narrow)) > len(lines) {
			return wrap(text, narrow+1)
		}
	}
	return lines
}
