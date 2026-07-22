package livetennisapi_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/livetennisapi/livetennisapi-go"
)

func ExampleNew() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	page, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{
		Status:     livetennisapi.StatusLive,
		ListParams: livetennisapi.ListParams{Limit: 10},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, match := range page.Data {
		fmt.Printf("%s: %s vs %s — %s\n",
			match.Tournament,
			match.Players.P1.Name,
			match.Players.P2.Name,
			match.Score, // nil-safe: prints "-" before a match starts
		)
	}
}

// Restrict results to one circuit. Each tour covers its singles and doubles
// draws.
func ExampleClient_ListMatches_byTour() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	page, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{
		Status: livetennisapi.StatusLive,
		Tour:   livetennisapi.TourWTA,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, match := range page.Data {
		fmt.Println(match.Tournament, match.Score)
	}
}

// An unrecognised tour is rejected rather than ignored, and the error names the
// values that would have worked.
func ExampleAPIError_allowedValues() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	_, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{
		Tour: livetennisapi.Tour("atpp"),
	})

	var apiErr *livetennisapi.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "bad_tour" {
		fmt.Println("allowed:", apiErr.AllowedValues)
	}
}

// DataCompleteness says how much biography the feed holds for a player, so an
// empty field can be read as "not in the feed" rather than "not fetched".
func ExamplePlayer_dataCompleteness() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	player, err := client.GetPlayer(context.Background(), 2317)
	if err != nil {
		log.Fatal(err)
	}

	dc := player.DataCompleteness
	switch {
	case dc == nil || !dc.Applicable():
		// A doubles team has no single biography to be complete.
		fmt.Println(player.Name, "— per-player completeness does not apply")
	case dc.Complete():
		fmt.Println(player.Name, "— full biography")
	default:
		fmt.Printf("%s: %d of %d fields known, missing %v\n",
			player.Name, *dc.Known, *dc.Of, dc.Missing)
	}
}

// An upcoming match has no score at all, so Match.Score is nil. The Score
// methods are nil-safe, but reading a field off it is not.
func ExampleClient_ListMatches_upcoming() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	page, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{
		Status: livetennisapi.StatusUpcoming,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, match := range page.Data {
		if match.Score == nil {
			fmt.Printf("%s starts at %s\n", match.Tournament, match.ScheduledTime)
			continue
		}
		fmt.Printf("%s is under way: %s\n", match.Tournament, match.Score)
	}
}

// A 403 means the key is valid but the plan is too low. Telling that apart
// from a rejected key is the difference between "upgrade" and "check your
// credentials".
func ExampleClient_GetMatchAnalysis_tierWall() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	analysis, err := client.GetMatchAnalysis(context.Background(), 918273)
	switch {
	case errors.Is(err, livetennisapi.ErrUpgradeRequired):
		var apiErr *livetennisapi.APIError
		errors.As(err, &apiErr)
		fmt.Printf("needs the %s tier\n", apiErr.RequiredTier)
		return
	case errors.Is(err, livetennisapi.ErrUnauthorized):
		fmt.Println("the API key was rejected")
		return
	case errors.Is(err, livetennisapi.ErrNotFound):
		fmt.Println("the model has not covered this match")
		return
	case err != nil:
		log.Fatal(err)
	}

	if analysis.Thesis != nil {
		fmt.Println(analysis.Thesis.Reasoning)
	}
}

// Back off when the budget runs out rather than hammering the window.
func ExampleClient_ListMatches_rateLimited() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	_, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{})

	var apiErr *livetennisapi.APIError
	if errors.As(err, &apiErr) && errors.Is(err, livetennisapi.ErrRateLimited) {
		wait := apiErr.RateLimit.RetryAfterOr(time.Minute)
		fmt.Printf("rate limited, waiting %s (limit %d/min)\n", wait, apiErr.RateLimit.LimitOr(30))
		time.Sleep(wait)
	}
}

// Watch the remaining budget on calls that succeed, where there is no error to
// carry it.
func ExampleWithRateLimitObserver() {
	client := livetennisapi.New(
		os.Getenv("LIVETENNISAPI_KEY"),
		livetennisapi.WithRateLimitObserver(func(rl livetennisapi.RateLimit) {
			if rl.RemainingOr(-1) >= 0 && rl.RemainingOr(0) < 5 {
				log.Printf("only %d requests left until %s", rl.RemainingOr(0), rl.Reset)
			}
		}),
	)

	if _, err := client.Health(context.Background()); err != nil {
		log.Fatal(err)
	}
}

// Paginate walks a whole collection, fetching each page only as it is needed.
func ExamplePaginate() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	players := livetennisapi.Paginate(context.Background(),
		func(ctx context.Context, p livetennisapi.ListParams) (*livetennisapi.Page[livetennisapi.Player], error) {
			return client.SearchPlayers(ctx, livetennisapi.SearchPlayersParams{
				Search:     "nadal",
				ListParams: p,
			})
		}, 0)

	for player, err := range players {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(player.Name)
	}
}

// Score.Games is player-major, so reading a set means taking one value from
// each side's list. GamesForSet does that for you.
func ExampleScore_GamesForSet() {
	score := &livetennisapi.Score{
		Games:  [][]int{{6, 3, 2}, {4, 6, 1}},
		Points: []string{"40", "AD"},
	}

	for set := range score.NumSets() {
		p1, p2, ok := score.GamesForSet(set)
		if !ok {
			continue
		}
		fmt.Printf("set %d: %d-%d\n", set+1, p1, p2)
	}
	fmt.Println(score)

	// Output:
	// set 1: 6-4
	// set 2: 3-6
	// set 3: 2-1
	// 6-4 3-6 2-1 (40-AD)
}

// A nil Score is what an upcoming match carries, and it formats cleanly.
func ExampleScore_String() {
	var notStarted *livetennisapi.Score
	fmt.Println(notStarted)

	// Output:
	// -
}
