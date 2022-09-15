package processors

import (
	"fmt"
	"github.com/s4kibs4mi/twilfe/config"
	"github.com/s4kibs4mi/twilfe/log"
	"github.com/s4kibs4mi/twilfe/models"
	"github.com/s4kibs4mi/twilfe/models/api_request"
	"github.com/s4kibs4mi/twilfe/services"
	"strings"
)

type IStateProcessor interface {
	Process(req *api_request.CustomerRequest) error
}

type StateProcessor struct {
	stateService    services.IStateService
	shopemaaService services.IShopemaaService
	twilioService   services.ITwilioService
	cfg             *config.Application
}

func (p *StateProcessor) Process(req *api_request.CustomerRequest) error {
	log.Log().Infoln("Message Received....")

	state, err := p.stateService.GetState(req.From)
	if err != nil {
		return err
	}

	switch state {
	case models.CustomerStateStart:
		return p.processStart(req)
	case models.CustomerStateChooseMenu:
		return p.processChooseMenu(req)
	case models.CustomerStateCheckout:
		return p.processCheckout(req)
	default:
		return p.processStart(req)
	}
}

func (p *StateProcessor) processStart(req *api_request.CustomerRequest) error {
	msg := "Ahoy, *" + req.ProfileName + "*\n"
	msg += "Welcome to *" + p.shopemaaService.GetName() + "*.\n\n"
	msg += "*Today's menu*,\n"

	products, err := p.shopemaaService.ListProducts()
	if err != nil {
		return err
	}

	for i, r := range products {
		products[i].Index = fmt.Sprintf("%d", i+1)
		msg += fmt.Sprintf("%d. %s - %.2f %s\n", i+1, r.Name, float64(r.Price)/float64(100), p.shopemaaService.GetCurrency())
	}

	msg += "\nReply with menu numbers (ie: 1,3)."

	if err := p.twilioService.Send(req.From, req.To, msg); err != nil {
		return err
	}

	if err := p.stateService.SetState(req.From, models.CustomerStateChooseMenu); err != nil {
		return err
	}
	if err := p.stateService.SetData(req.From, string(models.CustomerStateChooseMenu), products); err != nil {
		return err
	}

	return nil
}

func (p *StateProcessor) processChooseMenu(req *api_request.CustomerRequest) error {
	prevStateData, err := p.stateService.GetData(req.From, string(models.CustomerStateChooseMenu))
	if err != nil {
		return err
	}

	msg := "*Review Selected Items*\n\n"
	total := int64(0)

	var selectedProducts []string
	var products = prevStateData.([]models.Product)
	var selectedIndexes = strings.Split(req.Body, ",")
	for _, index := range selectedIndexes {
		for _, pr := range products {
			if pr.Index == strings.TrimSpace(index) {
				selectedProducts = append(selectedProducts, pr.ID)
				msg += fmt.Sprintf("%s - %.2f %s\n", pr.Name, float64(pr.Price)/float64(100), p.shopemaaService.GetCurrency())
				total += pr.Price
			}
		}
	}

	msg += fmt.Sprintf("\n*Total: %.2f %s*\n", float64(total)/float64(100), p.shopemaaService.GetCurrency())
	msg += "\nReply *1* to Confirm and *0* to Cancel"

	cartID, err := p.shopemaaService.AddToCart(selectedProducts)
	if err != nil {
		return err
	}

	if err := p.twilioService.Send(req.From, req.To, msg); err != nil {
		return err
	}

	if err := p.stateService.SetState(req.From, models.CustomerStateCheckout); err != nil {
		return err
	}
	if err := p.stateService.SetData(req.From, "cartID", cartID); err != nil {
		return err
	}

	return nil
}

func (p *StateProcessor) processCheckout(req *api_request.CustomerRequest) error {
	decision := strings.TrimSpace(req.Body)

	if decision == "1" {
		return p.processPlaceOrder(req)
	} else if decision == "0" {
		return p.processCancelOrder(req)
	} else {
		// TODO:
	}

	return nil
}

func (p *StateProcessor) processPlaceOrder(req *api_request.CustomerRequest) error {
	val, err := p.stateService.GetData(req.From, "cartID")
	if err != nil {
		return err
	}

	cartID := val.(string)

	names := strings.Split(req.ProfileName, "")
	lastName := names[0]
	if len(names) > 1 {
		lastName = names[1]
	}

	params := models.PlaceOrderParams{
		FirstName:  names[0],
		LastName:   lastName,
		CartID:     cartID,
		CustomerID: req.From,
		Phone:      req.From,
	}

	orderHash, err := p.shopemaaService.ConfirmOrder(&params)
	if err != nil {
		return err
	}

	msg := "Thanks for your order.\n"
	msg += fmt.Sprintf("OrderId: %s\n", orderHash)
	msg += "You will be notified when your order is ready.\nIn the mean time tap the below link to pay online or to check order details."

	if err := p.twilioService.Send(req.From, req.To, msg); err != nil {
		return err
	}

	orderUrl := fmt.Sprintf("%s/orders/%s", p.cfg.URL, orderHash)

	if err := p.twilioService.Send(req.From, req.To, orderUrl); err != nil {
		return err
	}
	if err := p.stateService.SetState(req.From, models.CustomerStateStart); err != nil {
		return err
	}

	return nil
}

func (p *StateProcessor) processCancelOrder(req *api_request.CustomerRequest) error {
	msg := "Order cancelled!"

	if err := p.twilioService.Send(req.From, req.To, msg); err != nil {
		return err
	}
	if err := p.stateService.SetState(req.From, models.CustomerStateStart); err != nil {
		return err
	}

	return nil
}

func NewStateProcessor(cfg *config.Application, stateService services.IStateService, shopemaaService services.IShopemaaService,
	twilioService services.ITwilioService) IStateProcessor {
	return &StateProcessor{
		cfg:             cfg,
		stateService:    stateService,
		shopemaaService: shopemaaService,
		twilioService:   twilioService,
	}
}
