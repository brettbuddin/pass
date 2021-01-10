prefix_path = "/api/v2"

upstream "widgets" {
    destination = "http://widgets.${namespace}.local" 
    owner = "Team A <team-a@company.com>"
    prefix_path = "/private"

    route {
        methods = ["GET"]
        path = "/widgets"
    }
}

upstream "bobs" {
    destination = "http://bobs.${namespace}.local"
    owner = "Team B <team-b@company.com>"
    flush_interval_ms = 1000

    route {
        methods = ["GET"]
        path = "/bobs/{[0-9]+}"
    }

    route {
        methods = ["GET", "POST"]
        path = "/bobs"
    }
}
